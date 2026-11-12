// Package headless provides unprivileged console control of the local daemon.
// It never opens the protected store or performs network/OS effects itself.
package headless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service is the published subset needed by the independent console foundation.
// Enrollment is enabled only after the signed bootstrap/default-target gates.
type Service interface {
	GetStatus(context.Context, *clientv0.GetStatusRequest, ...grpc.CallOption) (*clientv0.GetStatusResponse, error)
	Disconnect(context.Context, *clientv0.DisconnectRequest, ...grpc.CallOption) (*clientv0.DisconnectResponse, error)
}

var ErrPrerequisite = errors.New("подключение ещё не готово: требуются подписанный bootstrap сервера и серверное назначение дефолтной сети")

// Run has a finite deadline and performs no blind mutation retries. A successful
// Disconnect RPC alone is not evidence of a disconnected tunnel.
func Run(ctx context.Context, service Service, command string, out io.Writer) error {
	if service == nil || out == nil {
		return errors.New("локальная служба недоступна")
	}
	if command != "status" && command != "down" && command != "up" {
		return errors.New("команда должна быть up, status или down")
	}
	budget := 30 * time.Second
	if command == "up" {
		budget = 22 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	response, err := readStatus(ctx, service)
	if err != nil {
		return publicError(err)
	}
	snapshot := response.GetStatus()
	if snapshot == nil || snapshot.Metadata == nil || snapshot.Metadata.InstanceId == "" {
		return errors.New("служба вернула неполное состояние")
	}
	if command == "up" {
		up, ok := service.(UpService)
		if !ok {
			return ErrPrerequisite
		}
		return runUp(ctx, up, snapshot, out)
	}
	if command == "status" {
		return writeStatus(out, snapshot)
	}
	if snapshot.ActiveProfileId == "" && (snapshot.ServiceState != clientv0.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT || snapshot.ControlState != clientv0.ControlState_CONTROL_STATE_NOT_REGISTERED) {
		return errors.New("нет прав управлять этой установкой Client")
	}
	if snapshot.ConnectionPhase == clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED && snapshot.GetIntent().GetDesiredState() != clientv0.DesiredState_DESIRED_STATE_CONNECTED {
		return writeStatus(out, snapshot)
	}
	if snapshot.ActiveProfileId == "" {
		return errors.New("невозможно отключить соединение: активный профиль неизвестен")
	}
	_, err = service.Disconnect(ctx, &clientv0.DisconnectRequest{
		Mutation: &clientv0.MutationContext{ExpectedInstanceId: snapshot.Metadata.InstanceId, ExpectedRevision: snapshot.Metadata.Revision},
		Profile:  &clientv0.ProfileRef{ProfileId: snapshot.ActiveProfileId},
	})
	if err != nil {
		return publicError(err)
	}
	response, err = service.GetStatus(ctx, &clientv0.GetStatusRequest{})
	if err != nil {
		return publicError(err)
	}
	after := response.GetStatus()
	if after == nil || after.Metadata == nil || after.Metadata.InstanceId != snapshot.Metadata.InstanceId || after.ActiveProfileId != snapshot.ActiveProfileId || after.Metadata.Revision < snapshot.Metadata.Revision || after.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED || after.GetIntent().GetDesiredState() == clientv0.DesiredState_DESIRED_STATE_CONNECTED {
		return errors.New("отключение пока не подтверждено; проверьте endlessnet status")
	}
	return writeStatus(out, after)
}

func readStatus(ctx context.Context, service Service) (*clientv0.GetStatusResponse, error) {
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return service.GetStatus(call, &clientv0.GetStatusRequest{})
}

func writeStatus(out io.Writer, s *clientv0.Status) error {
	// Deliberately expose only typed, public state. Do not print raw RPC messages,
	// diagnostics, session fields, arbitrary server text or private configuration.
	_, err := fmt.Fprintf(out, "Служба: %s\nРегистрация: %s\nСоединение: %s\n", s.ServiceState, s.ControlState, s.ConnectionPhase)
	return err
}

func publicError(err error) error {
	if errors.Is(err, context.Canceled) {
		return errors.New("ожидание отменено; состояние службы проверьте командой endlessnet status")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("истекло время ожидания; результат проверьте командой endlessnet status")
	}
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated:
		return errors.New("нет прав управлять этой установкой Client")
	case codes.Aborted, codes.FailedPrecondition:
		return errors.New("состояние службы изменилось; проверьте endlessnet status перед повтором")
	case codes.Canceled:
		return errors.New("ожидание отменено; состояние службы проверьте командой endlessnet status")
	case codes.DeadlineExceeded:
		return errors.New("истекло время ожидания; результат проверьте командой endlessnet status")
	default:
		return errors.New("не удалось получить подтверждённый результат от службы Client")
	}
}
