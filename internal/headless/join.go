package headless

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// EnrollWithKey submits one server enrollment. Only the daemon stores the attempt.
func EnrollWithKey(ctx context.Context, client UpService, account, network string, input io.Reader, output io.Writer) error {
	for _, id := range []string{account, network} {
		if id == "" || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
			return errors.New("укажите Account и Network")
		}
	}
	const maxInput = 65536
	raw, err := io.ReadAll(io.LimitReader(input, maxInput+1))
	defer clear(raw)
	key := bytes.TrimSuffix(raw, []byte("\n"))
	if len(key) < len(raw) {
		key = bytes.TrimSuffix(key, []byte("\r"))
	}
	if err != nil || len(raw) > maxInput || len(key) == 0 || !utf8.Valid(key) || !bytes.Equal(bytes.TrimSpace(key), key) || bytes.IndexFunc(key, unicode.IsControl) >= 0 {
		return errors.New("ожидался один регистрационный ключ в стандартном вводе")
	}
	state, err := readStatus(ctx, client)
	if err != nil {
		return publicError(err)
	}
	snapshot := state.GetStatus()
	meta := snapshot.GetMetadata()
	if meta.GetInstanceId() == "" || meta.GetRevision() == 0 {
		return errors.New("служба вернула неполное состояние")
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return errors.New("не удалось определить имя компьютера")
	}
	req := &clientv0.EnrollRequest{
		Mutation:  &clientv0.MutationContext{ExpectedInstanceId: meta.InstanceId, ExpectedRevision: meta.Revision},
		RequestId: uuid.NewString(), Hostname: host, Mode: clientv0.EnrollmentMode_ENROLLMENT_MODE_SERVER,
		JoinTokenEnrollment: &clientv0.JoinTokenEnrollment{AccountId: account, NetworkId: network, JoinToken: string(key)},
	}
	if snapshot.ActiveProfileId != "" {
		req.Profile = &clientv0.ProfileRef{ProfileId: snapshot.ActiveProfileId}
	}
	call, cancel := context.WithTimeout(ctx, 15*time.Second)
	receipt, err := client.Enroll(call, req)
	cancel()
	attempt := receipt.GetAttempt()
	if err != nil {
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded:
			read, readErr := client.GetEnrollmentAttempt(ctx, &clientv0.GetEnrollmentAttemptRequest{
				Profile: req.Profile, Selector: &clientv0.GetEnrollmentAttemptRequest_RequestId{RequestId: req.RequestId},
			})
			if readErr != nil {
				return fmt.Errorf("результат заявки %s неизвестен; проверьте endlessnet status", req.RequestId)
			}
			attempt = read.GetAttempt()
		case codes.Unimplemented:
			return errors.New("локальная служба пока не поддерживает регистрацию по ключу")
		default:
			return publicError(err)
		}
	}
	if attempt.GetRequestId() != req.RequestId || attempt.Mode != req.Mode || attempt.Hostname != host ||
		attempt.GetAccountId() != account || attempt.GetNetworkId() != network ||
		req.Profile != nil && attempt.ProfileId != req.Profile.ProfileId {
		return errors.New("служба вернула другую попытку регистрации")
	}
	if err := checkAttempt(attempt, meta.InstanceId); err != nil {
		return err
	}
	switch attempt.Phase {
	case clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERING:
		if attempt.Restriction != nil {
			_, err = fmt.Fprintf(output, "Заявка %s сохранена. %s\n", req.RequestId, failureMessage(attempt.Restriction.ReasonKey))
			return err
		}
		_, err = fmt.Fprintf(output, "Заявка %s принята службой. Состояние доступно в endlessnet status.\n", req.RequestId)
	case clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERED:
		if attempt.GetNodeId() == "" {
			return errors.New("служба не подтвердила идентификатор узла")
		}
		_, err = fmt.Fprintf(output, "Регистрация подтверждена для заявки %s.\n", req.RequestId)
	default:
		return errors.New("служба вернула неожиданное состояние регистрации по ключу")
	}
	return err
}
