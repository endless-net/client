//go:build windows || linux || darwin

package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCLocalAcceptanceAndLostResponseRecovery(t *testing.T) {
	m := newRPCStoreTest(t)
	service := NewClientRPCService(m, &ipc.BuildIdentity{Version: "test"})
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-acceptance-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-rpc-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(dir); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(dir, "rpc.sock")
	}
	listener, err := local.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server := local.NewServer(service.Handler())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() {
		_ = server.Close()
		if err := <-done; err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	client, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	request := rpcCreateRequest(t, m)
	request.ControlOrigin = "https://control.example.test"
	info, err := client.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	support, err := client.GetSupportInfo(ctx, connect.NewRequest(&ipc.GetSupportInfoRequest{}))
	if err != nil || support.Msg.Info.ProductName != "EndlessNet" || support.Msg.Info.Runtime.Version != "test" || support.Msg.Info.Runtime.Architecture != runtime.GOARCH {
		t.Fatal("observer support metadata missing", err)
	}
	_, err = client.ListManagedSettings(ctx, connect.NewRequest(&ipc.ListManagedSettingsRequest{Profile: &ipc.ProfileRef{ProfileId: "not-observer-visible"}}))
	assertRPCFailure(t, err, rpcUnownedMissingResourceFailure(t, info))
	events, err := client.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	if !events.Receive() || events.Msg().Sequence != 1 || events.Msg().GetSnapshot() == nil {
		t.Fatalf("missing first native snapshot: %v", events.Err())
	}
	initialAccess := events.Msg().GetSnapshot().GetRuntime().GetCallerAccess()
	if initialAccess != info.CallerAccess {
		t.Fatal("opening snapshot disagrees with authenticated bootstrap access")
	}
	accepted, err := client.CreateProfile(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if m.store.Read().LocalOwnerID == "" {
		t.Fatal("transport did not propagate OS identity into durable owner")
	}
	uiClaim := &ipc.BuildIdentity{Version: "synthetic-ui-claim"}
	update, err := client.GetUpdateInfo(ctx, connect.NewRequest(&ipc.GetUpdateInfoRequest{ReportedUi: uiClaim}))
	if err != nil {
		t.Fatal("native update discovery read failed", err)
	}
	if update.Msg.Info.InstalledRuntime.Version != "test" || !proto.Equal(update.Msg.Info.ReportedUi, uiClaim) || update.Msg.Info.State != ipc.UpdateState_UPDATE_STATE_SOURCE_UNAVAILABLE || update.Msg.Info.Available != nil || update.Msg.Info.InstalledPair.State != ipc.CompatibilityState_COMPATIBILITY_STATE_UNKNOWN {
		t.Fatal("native update read fabricated release or pairing evidence")
	}
	wantAccess := ipc.Access_ACCESS_OWNER
	switch initialAccess {
	case ipc.Access_ACCESS_OBSERVER:
		if events.Receive() {
			t.Fatal("ownership claim continued the observer stream")
		}
		assertRPCFailure(t, events.Err(), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	case ipc.Access_ACCESS_ADMINISTRATOR:
		// An elevated runner remains administrator after claiming ownership.
		// Unchanged access must keep delivering typed changes, not force a reset.
		wantAccess = ipc.Access_ACCESS_ADMINISTRATOR
		if !events.Receive() || events.Msg().Sequence != 2 || events.Msg().GetStatusChanged() == nil || events.Msg().Metadata.Revision != accepted.Msg.Operation.Metadata.Revision {
			t.Fatalf("administrator lost typed status update: %v", events.Err())
		}
		if !events.Receive() || events.Msg().Sequence != 3 || !proto.Equal(events.Msg().GetOperationChanged(), accepted.Msg.Operation) {
			t.Fatalf("administrator lost operation update: %v", events.Err())
		}
		if !events.Receive() || events.Msg().Sequence != 4 || events.Msg().GetInvalidated().GetDomain() != ipc.Domain_DOMAIN_PROFILES {
			t.Fatalf("administrator lost profile invalidation: %v", events.Err())
		}
	default:
		t.Fatal("unexpected access for an unowned installation")
	}
	_ = events.Close()
	events, err = client.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if !events.Receive() || events.Msg().Sequence != 1 || events.Msg().GetSnapshot().GetRuntime().GetCallerAccess() != wantAccess || events.Msg().Metadata.Revision != accepted.Msg.Operation.Metadata.Revision {
		t.Fatalf("missing fresh committed owner snapshot: %v", events.Err())
	}
	_ = events.Close()
	lookup := &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: request.Mutation.RequestId}}
	// A newly attached consumer has only its original request ID, not an op ID.
	client.Close()
	recovered, err := client.GetOperation(ctx, connect.NewRequest(lookup))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(accepted.Msg.Operation, recovered.Msg.Operation) || recovered.Msg.Operation.Kind != ipc.OperationKind_OPERATION_KIND_CREATE_PROFILE {
		t.Fatal("reconnection did not recover the identified original operation")
	}
	replayed, err := client.CreateProfile(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replayed.Msg.Operation, recovered.Msg.Operation) || len(m.store.Read().RPCState.Profiles) != 1 {
		t.Fatal("transport retry ran the mutation twice")
	}
	logs, err := client.ListRecentLogs(ctx, connect.NewRequest(&ipc.ListRecentLogsRequest{Profile: &ipc.ProfileRef{ProfileId: accepted.Msg.Operation.ProfileId}}))
	if err != nil || len(logs.Msg.Logs) != 1 || !strings.Contains(logs.Msg.Logs[0].Message, "OPERATION_KIND_CREATE_PROFILE state=OPERATION_STATE_SUCCEEDED") || logs.Msg.Page.Metadata.InstanceId != m.instanceID {
		t.Fatal("native log source missing or replay duplicated committed creation", err)
	}
	request.DisplayName = "different payload"
	_, err = client.CreateProfile(ctx, connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	if len(m.store.Read().RPCState.Profiles) != 1 {
		t.Fatal("conflicting request performed side effects")
	}
	selection := &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: accepted.Msg.Operation.ProfileId}}
	_, err = client.SelectProfile(ctx, connect.NewRequest(selection))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	workerCtx, stopWorker := context.WithCancel(ctx)
	allowSwitch := make(chan struct{})
	workerDone, err := service.StartProfileWorker(workerCtx, ClientRPCProfileDriver{Lock: &sync.Mutex{},
		Logout: func(_ context.Context, cfg Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
			if cfg.NodeID == "" {
				t.Error("Logout lost registered node")
			}
			return "", checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true})
		},
		Stop: func(ctx context.Context) (ipc.ConnectionContinuity, error) {
			select {
			case <-allowSwitch:
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
			case <-ctx.Done():
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, ctx.Err()
			}
		},
		Start: func(_ context.Context, cfg Config) error {
			if cfg.NodeID == "" {
				t.Error("empty profile attempted connection")
			}
			return nil
		},
	})
	if err != nil {
		stopWorker()
		t.Fatal(err)
	}
	defer func() { stopWorker(); <-workerDone }()
	profiles, err := client.ListProfiles(ctx, connect.NewRequest(&ipc.ListProfilesRequest{}))
	if err != nil || len(profiles.Msg.Profiles) != 1 || profiles.Msg.Profiles[0].Selection.Availability != ipc.Availability_AVAILABILITY_AVAILABLE {
		t.Fatal("running worker not projected in profile selection", err)
	}
	switchEvents, err := client.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = switchEvents.Close() }()
	if !switchEvents.Receive() {
		t.Fatal(switchEvents.Err())
	}
	rebootstrapEnrollmentEvents := func() {
		t.Helper()
		// Capabilities are immutable opening context. Drain the previous
		// stream's terminal failure, then open a new stream without replaying
		// any already accepted command.
		for switchEvents.Receive() {
		}
		assertRPCFailure(t, switchEvents.Err(), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		_ = switchEvents.Close()
		info, err := client.Bootstrap(ctx)
		if err != nil || len(info.Capabilities) != 5 {
			t.Fatal("bootstrap did not expose ready profile and enrollment workers", err)
		}
		switchEvents, err = client.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
		if err != nil || !switchEvents.Receive() || switchEvents.Msg().Sequence != 1 || switchEvents.Msg().GetSnapshot() == nil {
			t.Fatal("worker transition did not produce a fresh opening snapshot", err)
		}
	}
	requestCtx, cancelRequest := context.WithCancel(ctx)
	selected, err := client.SelectProfile(requestCtx, connect.NewRequest(selection))
	cancelRequest()
	if err != nil {
		t.Fatal(err)
	}
	close(allowSwitch)
	switchCompleted := false
	for switchEvents.Receive() {
		op := switchEvents.Msg().GetOperationChanged()
		if op.GetId() == selected.Msg.Operation.Id && rpcOperationTerminal(op.State) {
			if op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.GetSelection().SelectedId != selection.Profile.ProfileId {
				t.Fatal("native worker selection failed")
			}
			switchCompleted = true
			break
		}
	}
	if !switchCompleted {
		t.Fatal("native worker did not publish terminal selection", switchEvents.Err())
	}
	disconnected, err := client.Disconnect(ctx, connect.NewRequest(&ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: selection.Profile}))
	if err != nil {
		t.Fatal(err)
	}
	if disconnected.Msg.Operation.Kind != ipc.OperationKind_OPERATION_KIND_DISCONNECT || disconnected.Msg.Operation.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
		t.Fatal("native Disconnect did not complete durable intent")
	}
	_, err = client.Connect(ctx, connect.NewRequest(&ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: selection.Profile}))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
	enrollment := &ipc.EnrollRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: selection.Profile,
		Mode: ipc.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE, Authentication: &ipc.EnrollRequest_BrowserLogin{BrowserLogin: true}}
	_, err = client.Enroll(ctx, connect.NewRequest(enrollment))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	enrollCtx, stopEnrollment := context.WithCancel(ctx)
	enrollDone, err := service.StartEnrollmentWorker(enrollCtx, func(_ context.Context, cfg Config, _ ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		cfg.EnrollmentRequestID = "native-approval"
		cfg.EnrollmentPollToken = "synthetic-private-poll-token"
		if err := save(cfg); err != nil {
			return nil, err
		}
		return &ipc.UserAction{Kind: ipc.UserAction_KIND_OPEN_BROWSER, BrowserUrl: "https://control.example.test/approve"}, nil
	})
	if err != nil {
		stopEnrollment()
		t.Fatal(err)
	}
	defer func() { stopEnrollment(); <-enrollDone }()
	rebootstrapEnrollmentEvents()
	enrollRequestCtx, cancelEnrollRequest := context.WithCancel(ctx)
	enrolled, err := client.Enroll(enrollRequestCtx, connect.NewRequest(enrollment))
	cancelEnrollRequest()
	if err != nil {
		t.Fatal(err)
	}
	waiting := false
	for switchEvents.Receive() {
		op := switchEvents.Msg().GetOperationChanged()
		if op.GetId() == enrolled.Msg.Operation.Id && op.State == ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER {
			if op.UserAction.GetKind() != ipc.UserAction_KIND_OPEN_BROWSER || strings.Contains(op.String(), "synthetic-private-poll-token") {
				t.Fatal("unsafe native approval event")
			}
			waiting = true
			break
		}
	}
	if !waiting {
		t.Fatal("native approval event missing", switchEvents.Err())
	}
	reattached, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer reattached.Close()
	if _, err := reattached.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	tracked, err := reattached.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: enrollment.Mutation.RequestId}}))
	if err != nil || tracked.Msg.Operation.Id != enrolled.Msg.Operation.Id || tracked.Msg.Operation.State != ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER {
		t.Fatal("reattachment lost approval operation", err)
	}
	approvalStatus, err := reattached.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{}))
	if err != nil || !proto.Equal(approvalStatus.Msg.Status.PendingAction, tracked.Msg.Operation.UserAction) || approvalStatus.Msg.Status.EnrollmentRequestId != "native-approval" {
		t.Fatal("GetStatus disagrees with recovered approval operation", err)
	}
	// Restart the executor while retaining the listener/journal and consumer.
	stopEnrollment()
	<-enrollDone
	resumeCtx, stopResumedEnrollment := context.WithCancel(ctx)
	defer stopResumedEnrollment()
	allowEnrollmentCompletion := make(chan struct{})
	policyFixture, policyKey := signedServiceDNSFixture(t)
	policyFixture.NetworkMap.Node.ID = "synthetic-test-node"
	resignApplicationMap(t, &policyFixture.NetworkMap, policyKey)
	resumedDone, err := service.StartEnrollmentWorker(resumeCtx, func(ctx context.Context, cfg Config, _ ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		select {
		case <-allowEnrollmentCompletion:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if cfg.EnrollmentRequestID != "native-approval" {
			t.Error("approval state not recovered")
		}
		cfg.EnrollmentRequestID, cfg.EnrollmentPollToken = "", ""
		cfg.NodeID = "synthetic-test-node"
		cfg.NodeCredential = "synthetic-logout-credential"
		cfg.NetworkID = policyFixture.NetworkMap.Network.ID
		cfg.MapRevision, cfg.MapGlobalRevision = policyFixture.NetworkMap.Network.Revision, policyFixture.NetworkMap.Revision.Global
		cfg.CachedMap, cfg.MapSigningTrust = &policyFixture.NetworkMap, policyFixture.SigningTrust
		return nil, save(cfg)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { stopResumedEnrollment(); <-resumedDone }()
	rebootstrapEnrollmentEvents()
	close(allowEnrollmentCompletion)
	registrationCompleted := false
	for switchEvents.Receive() {
		op := switchEvents.Msg().GetOperationChanged()
		if op.GetId() == enrolled.Msg.Operation.Id && rpcOperationTerminal(op.State) {
			if op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.GetEnrollment().GetNodeId() != "synthetic-test-node" {
				t.Fatal("native enrollment result missing")
			}
			registrationCompleted = true
			break
		}
	}
	if !registrationCompleted {
		t.Fatal("native enrollment completion event missing", switchEvents.Err())
	}
	completedStatus, err := reattached.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{}))
	if err != nil || completedStatus.Msg.Status.PendingAction != nil || completedStatus.Msg.Status.EnrollmentRequestId != "" {
		t.Fatal("completed enrollment retained approval action", err)
	}
	connected, err := client.Connect(ctx, connect.NewRequest(&ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: selection.Profile}))
	if err != nil {
		t.Fatal(err)
	}
	for switchEvents.Receive() {
		op := switchEvents.Msg().GetOperationChanged()
		if op.GetId() == connected.Msg.Operation.Id && rpcOperationTerminal(op.State) {
			if op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.Kind != ipc.OperationKind_OPERATION_KIND_CONNECT {
				t.Fatal("native Connect failed")
			}
			setRequest := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: selection.Profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT.Enum()}}
			setAccepted, err := client.SetPreferences(ctx, connect.NewRequest(setRequest))
			if err != nil {
				t.Fatal(err)
			}
			preferences, err := client.GetPreferences(ctx, connect.NewRequest(&ipc.GetPreferencesRequest{Profile: selection.Profile}))
			if err != nil || preferences.Msg.Preferences.Lifecycle.UiQuit.Requested == nil || preferences.Msg.Preferences.Lifecycle.UiQuit.Control.Source != ipc.SettingSource_SETTING_SOURCE_USER {
				t.Fatal("native user preference projection", err)
			}
			managed, err := client.ListManagedSettings(ctx, connect.NewRequest(&ipc.ListManagedSettingsRequest{Profile: selection.Profile}))
			if err != nil || len(managed.Msg.Settings) != 1 || managed.Msg.Settings[0].Key != ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT || managed.Msg.Settings[0].GetLifecycleValue() != preferences.Msg.Preferences.Lifecycle.UiQuit.Effective || managed.Msg.Settings[0].Control.Source != ipc.SettingSource_SETTING_SOURCE_USER || managed.Msg.Metadata.Revision != preferences.Msg.Preferences.Metadata.Revision {
				t.Fatal("managed projection disagrees with native preferences", err)
			}
			resetRequest := &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: selection.Profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT}}
			resetAccepted, err := client.ResetPreferences(ctx, connect.NewRequest(resetRequest))
			if err != nil {
				t.Fatal(err)
			}
			managed, err = client.ListManagedSettings(ctx, connect.NewRequest(&ipc.ListManagedSettingsRequest{Profile: selection.Profile}))
			if err != nil || managed.Msg.Settings[0].Control.Source != ipc.SettingSource_SETTING_SOURCE_DEFAULT {
				t.Fatal("reset retained managed user source", err)
			}
			// A delayed retransmission of an older successful setter must not
			// undo a newer reset, even though its CAS revision is now old.
			resetRevision := m.Metadata().Revision
			setReplay, err := client.SetPreferences(ctx, connect.NewRequest(setRequest))
			if err != nil || !proto.Equal(setReplay.Msg.Operation, setAccepted.Msg.Operation) {
				t.Fatal("late setter replay did not recover original operation", err)
			}
			resetReplay, err := client.ResetPreferences(ctx, connect.NewRequest(resetRequest))
			if err != nil || !proto.Equal(resetReplay.Msg.Operation, resetAccepted.Msg.Operation) {
				t.Fatal("reset replay did not recover original operation", err)
			}
			lookup, err := client.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: setRequest.Mutation.RequestId}}))
			if err != nil || !proto.Equal(lookup.Msg.Operation, setAccepted.Msg.Operation) {
				t.Fatal("preference request-ID recovery lost original result", err)
			}
			preferences, err = client.GetPreferences(ctx, connect.NewRequest(&ipc.GetPreferencesRequest{Profile: selection.Profile}))
			if err != nil || preferences.Msg.Preferences.Lifecycle.UiQuit.Requested != nil || preferences.Msg.Preferences.Lifecycle.UiQuit.Control.Source != ipc.SettingSource_SETTING_SOURCE_DEFAULT || m.Metadata().Revision != resetRevision {
				t.Fatal("late retry reapplied user override or advanced state", err)
			}
			quit, err := client.NotifyLifecycle(ctx, connect.NewRequest(&ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: selection.Profile, Event: ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT}))
			if err != nil || quit.Msg.Operation.Kind != ipc.OperationKind_OPERATION_KIND_NOTIFY_LIFECYCLE || quit.Msg.Operation.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				t.Fatal("native lifecycle notification", err)
			}
			logoutCtx, cancelLogout := context.WithCancel(ctx)
			logout, err := client.Logout(logoutCtx, connect.NewRequest(&ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: selection.Profile}))
			cancelLogout()
			if err != nil {
				t.Fatal(err)
			}
			for switchEvents.Receive() {
				op := switchEvents.Msg().GetOperationChanged()
				if op.GetId() == logout.Msg.Operation.Id && rpcOperationTerminal(op.State) {
					if op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.GetCleanup().Outcome != ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_CONFIRMED || m.store.Read().NodeCredential != "" {
						t.Fatal("native Logout did not confirm cleanup")
					}
					return
				}
			}
			t.Fatal("Logout completion event missing", switchEvents.Err())
		}
	}
	t.Fatal("Connect terminal event missing", switchEvents.Err())
}
