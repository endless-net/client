//go:build windows && integration

package localapi

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"golang.org/x/sys/windows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type faultBackend struct {
	state.Backend
	lose atomic.Bool
}
type faultSlot struct {
	state.Slot
	lose *atomic.Bool
}

func (s faultSlot) Replace(data []byte) error {
	if err := s.Slot.Replace(data); err != nil {
		return err
	}
	if s.lose.Swap(false) {
		return state.ErrUncertain
	}
	return nil
}
func (b *faultBackend) Exclusive(ctx context.Context, fn func(state.Slot) error) error {
	return b.Backend.Exclusive(ctx, func(s state.Slot) error { return fn(faultSlot{Slot: s, lose: &b.lose}) })
}

func nativeStore(t *testing.T) (*state.Store, *faultBackend, string) {
	t.Helper()
	directory := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	b, err := state.OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	p, err := state.NewDPAPI(installationID)
	if err != nil {
		t.Fatal(err)
	}
	fault := &faultBackend{Backend: b}
	s, err := state.New(fault, p, installationID, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, fault, sid
}

func nativeClient(t *testing.T, c *Controller, sid string) (clientv0.ClientServiceClient, func()) {
	t.Helper()
	config := localipc.PipeConfig{InstanceID: c.config.RuntimeID, ServerSID: sid, ContractDigest: c.config.ContractDigest, HandshakeTimeout: 2 * time.Second}
	listener, err := localipc.Listen(config, []string{sid})
	if err != nil {
		t.Fatal(err)
	}
	server, err := localipc.NewServer(config, c.store.Owner, c, 65536, 16, localipc.Observation{Unary: func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		return next(ctx, request)
	}, Stream: func(server any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		return next(server, stream)
	}})
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := localipc.Connect(ctx, config)
	if err != nil {
		server.Stop()
		t.Fatal(err)
	}
	return clientv0.NewClientServiceClient(conn), func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
		server.Stop()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("local controller server did not stop")
		}
	}
}

func TestWindowsControllerDurableClaimAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit authenticated pipe + DPAPI controller fixture")
	}
	s, fault, sid := nativeStore(t)
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	cfg := config(&now)
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeID = id
	runner := &testRunner{store: s}
	c, err := New(s, runner, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client, stop := nativeClient(t, c, sid)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := request(1)
	r.Mutation.ExpectedInstanceId = cfg.RuntimeID
	bootstrap, err := client.GetRuntimeInfo(ctx, &clientv0.GetRuntimeInfoRequest{})
	if err != nil || len(bootstrap.Runtime.Capabilities) != 1 || bootstrap.Runtime.Capabilities[0].Capability != clientv0.Capability_CAPABILITY_ENROLLMENT || bootstrap.Runtime.Capabilities[0].Restriction.Availability != clientv0.Availability_AVAILABILITY_AVAILABLE {
		stop()
		t.Fatal("real pipe did not advertise initial enrollment claim", err)
	}
	beforeClaim, err := s.Load(ctx)
	if err != nil || beforeClaim.OwnerSID != "" || beforeClaim.Revision != 1 || runner.calls != 0 {
		stop()
		t.Fatal("RuntimeInfo bootstrap claimed owner or changed state", err)
	}
	initial, err := client.GetStatus(ctx, &clientv0.GetStatusRequest{})
	if err != nil || initial.Status.ActiveProfileId != "" || initial.Status.Metadata.InstanceId != cfg.RuntimeID {
		stop()
		t.Fatal("initial observer snapshot failed", err)
	}
	r.Mutation.ExpectedRevision = initial.Status.Metadata.Revision
	fault.lose.Store(true)
	if _, err = client.Enroll(ctx, r); status.Code(err) != codes.Unavailable || runner.calls != 0 {
		stop()
		t.Fatal("uncertain DPAPI persistence dispatched network work")
	}
	got, err := client.GetEnrollmentAttempt(ctx, getRequest())
	if err != nil {
		stop()
		t.Fatal(err)
	}
	stored, err := s.Load(ctx)
	if err != nil || stored.OwnerSID != sid || stored.Profiles[defaultID].Attempt.Owner != sid {
		stop()
		t.Fatal("owner claim not bound to real OS SID", err)
	}
	if got.Attempt.NodeId != nil || got.Attempt.BrowserAction != nil {
		stop()
		t.Fatal("local preparation claimed server effect")
	}

	// The current OS token determines whether the real privileged RPC is available.
	observed, err := client.GetServerIdentity(ctx, &clientv0.GetServerIdentityRequest{})
	if err != nil {
		stop()
		t.Fatal(err)
	}
	runtime, err := client.GetRuntimeInfo(ctx, &clientv0.GetRuntimeInfoRequest{})
	if err != nil {
		stop()
		t.Fatal(err)
	}
	confirmation := &clientv0.TrustServerIdentityRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: cfg.RuntimeID, ExpectedRevision: observed.Metadata.Revision}, ConfirmedControlOrigin: observed.Identity.ControlOrigin, ConfirmedKeyId: observed.Identity.AnnouncedKeyId, ConfirmedAnnouncementId: observed.Identity.AnnouncementId}
	_, err = client.TrustServerIdentity(ctx, confirmation)
	if runtime.Runtime.CallerAccess == clientv0.Access_ACCESS_ADMINISTRATOR {
		if err != nil {
			stop()
			t.Fatal(err)
		}
	} else {
		if status.Code(err) != codes.PermissionDenied {
			stop()
			t.Fatal("non-admin trust RPC accepted", err)
		}
		// Only fixture setup seeds public trust when this process is not elevated.
		// Positive OS-admin authorization is not claimed for a non-admin test token.
		a, e := c.announcement(ctx, cfg.DefaultControlOrigin)
		if e != nil {
			stop()
			t.Fatal(e)
		}
		e = s.Update(ctx, stored.Revision, func(s *state.State) error {
			p := s.Profiles[defaultID]
			p.Trust = state.ServerTrust{KeyID: a.KeyID, AnnouncementID: a.ID, Bundle: a.Bundle}
			s.Profiles[defaultID] = p
			return nil
		})
		if e != nil {
			stop()
			t.Fatal(e)
		}
		if _, e = client.GetEnrollmentAttempt(ctx, getRequest()); e != nil {
			stop()
			t.Fatal(e)
		}
	}
	callsBeforeRestart := runner.calls
	stop()
	newID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeID = newID
	restarted, err := New(s, runner, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client, stop = nativeClient(t, restarted, sid)
	defer stop()
	if _, err = client.Enroll(ctx, r); status.Code(err) != codes.Aborted {
		t.Fatal("previous runtime mutation accepted")
	}
	replay, err := client.GetEnrollmentAttempt(ctx, getRequest())
	if err != nil || replay.Attempt.AttemptId != got.Attempt.AttemptId || replay.Attempt.Metadata.InstanceId != newID || !replay.Attempt.Deadline.AsTime().Equal(got.Attempt.Deadline.AsTime()) {
		t.Fatal("native restart changed attempt/deadline", err)
	}
	if runner.calls != callsBeforeRestart+1 {
		t.Fatal("persisted attempt did not resume after restart")
	}
}
