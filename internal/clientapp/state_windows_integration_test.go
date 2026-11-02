//go:build windows && integration

package clientapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	"go.uber.org/fx"
	"golang.org/x/sys/windows"
)

func stateFixtureConfig(t *testing.T) Config {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	directory := t.TempDir()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	environment := configEnvironment()
	environment["ENDLESSNET_CLIENT_STATE_DIRECTORY"] = directory
	environment["ENDLESSNET_CLIENT_SERVICE_SID"] = sid
	environment["ENDLESSNET_CLIENT_ALLOWED_SIDS"] = sid
	config, err := ParseWindows(environment)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func seedState(t *testing.T, c Config) state.State {
	t.Helper()
	backend, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	}()
	protector, err := state.NewDPAPI(c.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.New(backend, protector, c.InstallationID, c.MaxStatePlainBytes, c.MaxStateCipherBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(budget(t)); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(budget(t), 1, func(s *state.State) error {
		keys, err := state.GenerateKeys()
		if err != nil {
			return err
		}
		fingerprint, err := keys.Fingerprint()
		if err != nil {
			return err
		}
		a, err := enrollment.New(enrollment.Binding{AttemptID: "487fb261-c50e-4fad-8e3a-47622509959b", RequestID: "b8c8f430-ce98-444d-84c5-61dcd346b4b5", ProfileID: c.DefaultProfileID, Owner: c.ServiceSID, ControlOrigin: c.ControlOrigin, DeviceKeyHash: fingerprint}, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
		if err != nil {
			return err
		}
		s.OwnerSID, s.ActiveProfileID = c.ServiceSID, c.DefaultProfileID
		s.Profiles[c.DefaultProfileID] = state.Profile{ID: c.DefaultProfileID, DisplayName: "Fixture", ControlOrigin: c.ControlOrigin, Keys: keys, Attempt: &a, Secrets: state.RemoteSecrets{RequestProof: "fixture-request-proof-00000000000000000"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load(budget(t))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestProtectedStateFxRestartKeepsOriginalCiphertextAndIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows DPAPI application lifecycle fixture")
	}
	c := stateFixtureConfig(t)
	original := seedState(t, c)
	cipher, err := os.ReadFile(filepath.Join(c.StateDirectory, "state.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var resource *ProtectedState
		app := fx.New(fx.NopLogger, fx.Invoke(func(lc fx.Lifecycle) error {
			var err error
			resource, err = NewProtectedState(lc, testSignals(t), c)
			return err
		}))
		if err := app.Err(); err != nil {
			t.Fatal(err)
		}
		if _, err := resource.Store().Load(budget(t)); !errors.Is(err, ErrProtectedState) {
			t.Fatal("state accessible before startup", err)
		}
		if err := app.Start(budget(t)); err != nil {
			t.Fatal(err)
		}
		current, err := resource.Store().Load(budget(t))
		if err != nil || !reflect.DeepEqual(current, original) {
			t.Fatal("restart changed identity/binding/deadlines", err)
		}
		if err := app.Stop(budget(t)); err != nil {
			t.Fatal(err)
		}
		if _, err := resource.Store().Load(budget(t)); !errors.Is(err, ErrProtectedState) {
			t.Fatal("state accessible after stop", err)
		}
	}
	after, err := os.ReadFile(filepath.Join(c.StateDirectory, "state.bin"))
	if err != nil || !bytes.Equal(cipher, after) {
		t.Fatal("startup rewrote protected state", err)
	}
}

func TestProtectedStateFxMigratesLegacyWithoutChangingIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("separate isolated Windows DPAPI migration/lifecycle fixture")
	}
	c := stateFixtureConfig(t)
	original := seedState(t, c)
	legacy := original
	legacy.Version = 1
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	protector, err := state.NewDPAPI(c.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := protector.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
	if err != nil {
		t.Fatal(err)
	}
	err = backend.Exclusive(budget(t), func(slot state.Slot) error { return slot.Replace(cipher) })
	if closeErr := backend.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	var resource *ProtectedState
	app := fx.New(fx.NopLogger, fx.Invoke(func(lc fx.Lifecycle) error {
		var err error
		resource, err = NewProtectedState(lc, testSignals(t), c)
		return err
	}))
	if err := app.Start(budget(t)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := app.Stop(budget(t)); err != nil {
			t.Error(err)
		}
	}()
	current, err := resource.Store().Load(budget(t))
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != state.CurrentVersion || current.Revision != original.Revision+1 || current.Profiles[c.DefaultProfileID].Connection != nil {
		t.Fatal("startup did not perform exactly one data migration")
	}
	current.Revision = original.Revision
	if !reflect.DeepEqual(current, original) {
		t.Fatal("migration changed owner/key/attempt binding")
	}
	after, err := os.ReadFile(filepath.Join(c.StateDirectory, "state.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(after, cipher) || bytes.Contains(after, original.Profiles[c.DefaultProfileID].Keys.WireGuardPrivate) {
		t.Fatal("migration did not reseal or exposed key material")
	}
}

func TestProtectedStateStartupFailureNeverResets(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows DPAPI startup refusal fixture")
	}
	for _, mode := range []string{"missing", "corrupt", "wrong_installation", "wrong_service", "configured_limit", "later_hook_failure"} {
		t.Run(mode, func(t *testing.T) {
			c := stateFixtureConfig(t)
			if mode != "missing" {
				_ = seedState(t, c)
			}
			path := filepath.Join(c.StateDirectory, "state.bin")
			if mode == "corrupt" {
				if err := os.WriteFile(path, []byte("invalid ciphertext"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(path)
			if mode == "wrong_installation" {
				c.InstallationID = "9bcc14eb-ef8d-4a6b-afef-955691ea4624"
			}
			if mode == "wrong_service" {
				c.ServiceSID = "S-1-5-19"
			}
			if mode == "configured_limit" {
				c.MaxDisplayNameBytes = 1
				c.ProfileDisplayName = "F"
			}
			var resource *ProtectedState
			app := fx.New(fx.NopLogger, fx.Invoke(func(lc fx.Lifecycle) error {
				var err error
				resource, err = NewProtectedState(lc, testSignals(t), c)
				if mode == "later_hook_failure" {
					lc.Append(fx.Hook{OnStart: func(context.Context) error { return errors.New("synthetic startup failure") }})
				}
				return err
			}))
			if err := app.Err(); err != nil {
				t.Fatal(err)
			}
			if err := app.Start(budget(t)); err == nil {
				t.Fatal("invalid source admitted startup")
			}
			if _, err := resource.Store().Load(budget(t)); !errors.Is(err, ErrProtectedState) {
				t.Fatal("failed source still admitted operations", err)
			}
			after, err := os.ReadFile(path)
			if mode == "missing" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing state initialized", err)
				}
			} else if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed startup reset or changed ciphertext", err)
			}
		})
	}
}

func TestProtectedStateStopCancelsTransactionsAndBoundsUnresponsiveCallback(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows protected state cancellation fixture")
	}
	c := stateFixtureConfig(t)
	_ = seedState(t, c)
	h := &hooks{}
	resource, err := NewProtectedState(h, testSignals(t), c)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	entered, release, completed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		completed <- resource.gate.Exclusive(context.Background(), func(state.Slot) error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.hook.OnStop(ctx); err != ErrProtectedState {
		t.Fatal("unresponsive transaction claimed clean stop", err)
	}
	if _, err := resource.Store().Load(budget(t)); !errors.Is(err, ErrProtectedState) {
		t.Fatal("new operations admitted after stop", err)
	}
	close(release)
	select {
	case <-completed:
	case <-budget(t).Done():
		t.Fatal("transaction did not finish")
	}
	select {
	case <-resource.gate.closed:
	case <-budget(t).Done():
		t.Fatal("backend handles not closed")
	}
	if err := h.hook.OnStop(budget(t)); err != ErrProtectedState {
		t.Fatal("stop failure forgotten", err)
	}
}

func TestProtectedStateStopCancelsActualWindowsLockWait(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows state lock cancellation fixture")
	}
	c := stateFixtureConfig(t)
	_ = seedState(t, c)
	h := &hooks{}
	resource, err := NewProtectedState(h, testSignals(t), c)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	external, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := external.Close(); err != nil {
			t.Error(err)
		}
	}()
	entered, release, held := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		held <- external.Exclusive(context.Background(), func(state.Slot) error { close(entered); <-release; return nil })
	}()
	defer func() {
		close(release)
		select {
		case err := <-held:
			if err != nil {
				t.Error(err)
			}
		case <-budget(t).Done():
			t.Error("external fixture lock did not finish")
		}
	}()
	<-entered
	loaded := make(chan error, 1)
	go func() { _, err := resource.Store().Load(context.Background()); loaded <- err }()
	// Observe the actual admitted transaction before Stop, so a pre-start
	// refusal cannot accidentally satisfy the cancellation assertion.
	wait := time.NewTicker(time.Millisecond)
	defer wait.Stop()
	deadline := budget(t)
	for {
		resource.gate.mu.Lock()
		active := resource.gate.active
		resource.gate.mu.Unlock()
		if active == 1 {
			break
		}
		select {
		case <-wait.C:
		case <-deadline.Done():
			t.Fatal("state transaction was not admitted")
		}
	}
	if err := h.hook.OnStop(budget(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-loaded:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("OS lock wait did not receive lifecycle cancellation", err)
		}
	case <-budget(t).Done():
		t.Fatal("state lock wait outlived stop")
	}
	// The external fixture owns its independent lock and releases it itself.
	// Client lifecycle does not close or unlock another backend's handles.
}
