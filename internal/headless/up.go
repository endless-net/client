package headless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UpService uses the published owner-only projection, never a local journal or
// protected state. Login/default-target authority remains in the daemon.
type UpService interface {
	Service
	Enroll(context.Context, *clientv0.EnrollRequest, ...grpc.CallOption) (*clientv0.EnrollResponse, error)
	GetEnrollmentAttempt(context.Context, *clientv0.GetEnrollmentAttemptRequest, ...grpc.CallOption) (*clientv0.GetEnrollmentAttemptResponse, error)
	Connect(context.Context, *clientv0.ConnectRequest, ...grpc.CallOption) (*clientv0.ConnectResponse, error)
}

func runUp(ctx context.Context, service UpService, initial *clientv0.Status, out io.Writer) error {
	if initial.ControlState == clientv0.ControlState_CONTROL_STATE_REGISTERED || initial.ControlState == clientv0.ControlState_CONTROL_STATE_READY {
		return connectUp(ctx, service, initial, out)
	}
	var attempt *clientv0.EnrollmentAttempt
	if initial.ActiveProfileId != "" {
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		current, err := service.GetEnrollmentAttempt(call, &clientv0.GetEnrollmentAttemptRequest{
			Profile:  &clientv0.ProfileRef{ProfileId: initial.ActiveProfileId},
			Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: true},
		})
		cancel()
		if err != nil && status.Code(err) != codes.NotFound {
			return publicError(err)
		}
		attempt = current.GetAttempt()
	}
	request := &clientv0.EnrollRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: initial.Metadata.InstanceId, ExpectedRevision: initial.Metadata.Revision}}
	if attempt != nil {
		if err := checkAttempt(attempt, initial.Metadata.InstanceId); err != nil {
			return err
		}
		if attempt.ProfileId != initial.ActiveProfileId || attempt.Metadata.Revision < initial.Metadata.Revision {
			return errors.New("профиль изменился; повторите endlessnet up после проверки status")
		}
		request.Mutation.ExpectedRevision = attempt.Metadata.Revision
		request.Profile = &clientv0.ProfileRef{ProfileId: attempt.ProfileId}
		request.RequestId, request.Mode, request.Hostname = attempt.RequestId, attempt.Mode, attempt.Hostname
	} else {
		hostname, err := os.Hostname()
		if err != nil || hostname == "" {
			return errors.New("не удалось определить имя компьютера")
		}
		request.RequestId, request.Mode, request.Hostname = uuid.NewString(), clientv0.EnrollmentMode_ENROLLMENT_MODE_WORKSTATION, hostname
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	started, err := service.Enroll(call, request)
	cancel()
	if err != nil {
		// The daemon may have committed before losing its reply. Read once using
		// the same UUID; do not issue another mutation or generate a new attempt.
		call, cancel = context.WithTimeout(ctx, 30*time.Second)
		observed, readErr := service.GetEnrollmentAttempt(call, &clientv0.GetEnrollmentAttemptRequest{Profile: request.Profile, Selector: &clientv0.GetEnrollmentAttemptRequest_RequestId{RequestId: request.RequestId}})
		cancel()
		if readErr != nil || observed.GetAttempt() == nil {
			return publicError(err)
		}
		attempt = observed.Attempt
	} else {
		attempt = started.GetAttempt()
	}
	lastURL := ""
	for {
		if err := checkAttempt(attempt, initial.Metadata.InstanceId); err != nil {
			return err
		}
		if attempt.RequestId != request.RequestId || attempt.Mode != request.Mode || attempt.Hostname != request.Hostname || request.Profile != nil && attempt.ProfileId != request.Profile.ProfileId {
			return errors.New("служба вернула другую попытку; автоматический повтор остановлен")
		}
		if attempt.Restriction != nil {
			if strings.Contains(attempt.Restriction.ReasonKey, "server_identity") {
				return errors.New("не удалось проверить сервер EndlessNet; вход не начат")
			}
			return errors.New("регистрация пока недоступна; проверьте состояние сервера и endlessnet status")
		}
		if attempt.Phase == clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERED {
			current, err := readStatus(ctx, service)
			if err != nil {
				return publicError(err)
			}
			snapshot := current.GetStatus()
			if snapshot == nil || snapshot.GetMetadata().GetInstanceId() != initial.Metadata.InstanceId || snapshot.ActiveProfileId != attempt.ProfileId || snapshot.ControlState != clientv0.ControlState_CONTROL_STATE_REGISTERED {
				return errors.New("регистрация ещё не подтверждена локальной службой")
			}
			return connectUp(ctx, service, snapshot, out)
		}
		if action := attempt.BrowserAction; action != nil && action.VerificationUrl != lastURL {
			u, err := url.Parse(action.VerificationUrl)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.IndexFunc(action.VerificationUrl, unicode.IsControl) >= 0 || action.ExpiresAt == nil || !action.ExpiresAt.IsValid() || !time.Now().Before(action.ExpiresAt.AsTime()) {
				return errors.New("служба вернула некорректную ссылку входа")
			}
			if _, err := fmt.Fprintln(out, "Подтвердите добавление компьютера в браузере (можно на другом устройстве):\n"+action.VerificationUrl); err != nil {
				return err
			}
			lastURL = action.VerificationUrl
		}
		if attempt.PollAfterSeconds < 1 || attempt.PollAfterSeconds > 30 {
			return errors.New("служба вернула некорректный интервал ожидания")
		}
		timer := time.NewTimer(time.Duration(attempt.PollAfterSeconds) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return publicError(ctx.Err())
		case <-timer.C:
		}
		call, cancel = context.WithTimeout(ctx, 30*time.Second)
		observed, err := service.GetEnrollmentAttempt(call, &clientv0.GetEnrollmentAttemptRequest{Profile: &clientv0.ProfileRef{ProfileId: attempt.ProfileId}, Selector: &clientv0.GetEnrollmentAttemptRequest_RequestId{RequestId: request.RequestId}})
		cancel()
		if err != nil {
			return publicError(err)
		}
		attempt = observed.GetAttempt()
	}
}

func checkAttempt(a *clientv0.EnrollmentAttempt, runtime string) error {
	if a == nil || a.GetMetadata().GetInstanceId() != runtime || a.Metadata.Revision == 0 || a.RequestId == "" || a.AttemptId == "" || a.ProfileId == "" || a.Mode < clientv0.EnrollmentMode_ENROLLMENT_MODE_WORKSTATION || a.Mode > clientv0.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE || a.Hostname == "" || a.Deadline == nil || !a.Deadline.IsValid() {
		return errors.New("неполные исходные данные регистрации; автоматическое восстановление остановлено")
	}
	if !time.Now().Before(a.Deadline.AsTime()) || a.Phase == clientv0.EnrollmentPhase_ENROLLMENT_PHASE_EXPIRED || a.Phase == clientv0.EnrollmentPhase_ENROLLMENT_PHASE_CANCELLED || a.Phase == clientv0.EnrollmentPhase_ENROLLMENT_PHASE_DENIED {
		return errors.New("попытка регистрации завершена; новая попытка автоматически не создаётся")
	}
	return nil
}

func connectUp(ctx context.Context, service UpService, before *clientv0.Status, out io.Writer) error {
	if before.ActiveProfileId == "" || before.Metadata == nil || before.Metadata.Revision == 0 {
		return errors.New("нет зарегистрированного активного профиля")
	}
	var connectErr error
	if !connectionReady(before) || before.GetIntent().GetDesiredState() != clientv0.DesiredState_DESIRED_STATE_CONNECTED {
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := service.Connect(call, &clientv0.ConnectRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: before.Metadata.InstanceId, ExpectedRevision: before.Metadata.Revision}, Profile: &clientv0.ProfileRef{ProfileId: before.ActiveProfileId}})
		cancel()
		if err != nil && status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded && status.Code(err) != codes.Canceled {
			if len(before.Failures) > 0 {
				if writeErr := writeStatus(out, before); writeErr != nil {
					return writeErr
				}
			}
			return publicError(err)
		}
		connectErr = err
	}
	response, err := readStatus(ctx, service)
	if err != nil {
		return publicError(err)
	}
	after := response.GetStatus()
	if after == nil || after.GetMetadata().GetInstanceId() != before.Metadata.InstanceId || after.Metadata.Revision < before.Metadata.Revision || after.ActiveProfileId != before.ActiveProfileId || (after.ControlState != clientv0.ControlState_CONTROL_STATE_REGISTERED && after.ControlState != clientv0.ControlState_CONTROL_STATE_READY) || !connectionReady(after) || after.GetIntent().GetDesiredState() != clientv0.DesiredState_DESIRED_STATE_CONNECTED {
		if after != nil && after.GetMetadata().GetInstanceId() == before.Metadata.InstanceId && after.Metadata.Revision >= before.Metadata.Revision && after.ActiveProfileId == before.ActiveProfileId && len(after.Failures) > 0 {
			if err := writeStatus(out, after); err != nil {
				return err
			}
		}
		if connectErr != nil {
			return publicError(connectErr)
		}
		return errors.New("соединение пока не подтверждено; проверьте endlessnet status")
	}
	return writeStatus(out, after)
}

func connectionReady(s *clientv0.Status) bool {
	if s == nil {
		return false
	}
	if s.ConnectionPhase == clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED {
		return true
	}
	return s.ServiceState == clientv0.ServiceState_SERVICE_STATE_LOCAL_READY && s.ConnectionPhase == clientv0.ConnectionPhase_CONNECTION_PHASE_LOCAL_READY && s.ControlState == clientv0.ControlState_CONTROL_STATE_READY && s.PeerCount == 0 && s.MapRevision > 0 && len(s.OverlayAddresses) > 0 && s.Agent != nil && s.Agent.SnapshotState == clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT && s.Agent.DirectPathCount == 0 && s.Agent.RelayPathCount == 0
}
