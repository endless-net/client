//go:build windows && integration

package clientapp

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type ownerChainLostReply struct {
	next http.RoundTripper
	lost atomic.Bool
}

// The native session stays inside the private Windows driver. The UI sees only
// the already published registration projection; this is a fresh server read,
// not a second interpretation of the local credential or a database row count.
func assertOwnerChainNodeList(t *testing.T, client *http.Client, profile state.Profile) {
	t.Helper()
	if profile.Attempt == nil || profile.Attempt.Phase != enrollment.Registered || profile.Secrets.Access == "" {
		t.Fatal("registered native profile required for fresh node list")
	}
	get := func(network, access string) (int, []byte) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, profile.ControlOrigin+"/networks/"+url.PathEscape(network)+"/nodes", nil)
		if err != nil {
			t.Fatal("node list request invalid")
		}
		request.Header.Set("Cache-Control", "no-cache")
		if access != "" {
			request.Header.Set("Authorization", "Bearer "+access)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("fresh node list unavailable")
		}
		defer func() { _ = response.Body.Close() }()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
		if err != nil || len(raw) > 65536 {
			t.Fatal("fresh node list exceeded bound or failed")
		}
		return response.StatusCode, raw
	}
	for _, denied := range []struct{ network, access string }{
		{profile.Attempt.NetworkID, ""}, {"foreign-network", profile.Secrets.Access},
	} {
		status, raw := get(denied.network, denied.access)
		if (status != http.StatusUnauthorized && status != http.StatusForbidden && status != http.StatusNotFound) || bytes.Contains(raw, []byte(profile.Attempt.NodeID)) {
			t.Fatal("unauthorized node list exposed registration", status)
		}
	}
	status, raw := get(profile.Attempt.NetworkID, profile.Secrets.Access)
	if status != http.StatusOK {
		t.Fatal("fresh authorized node list rejected", status)
	}
	var nodes []coordinator.Node
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&nodes) != nil || decoder.Decode(new(any)) != io.EOF || nodes == nil {
		t.Fatal("published Node[] decode failed")
	}
	fingerprint, err := profile.Keys.Fingerprint()
	if err != nil {
		t.Fatal("protected device identity invalid")
	}
	matches := 0
	for _, node := range nodes {
		if node.NetworkID != profile.Attempt.NetworkID {
			t.Fatal("fresh list included another Network")
		}
		if node.ID == profile.Attempt.NodeID || node.DeviceFingerprint == hex.EncodeToString(fingerprint[:]) {
			if node.ID != profile.Attempt.NodeID || node.DeviceFingerprint != hex.EncodeToString(fingerprint[:]) || node.UserID != profile.Secrets.UserID {
				t.Fatal("fresh list identity does not match Client registration")
			}
			matches++
		}
	}
	if matches != 1 {
		t.Fatal("fresh list does not contain exactly one matching Client Node", matches)
	}
}

func (f *ownerChainLostReply) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/nodes/register" && f.lost.Load() {
		return nil, errors.New("isolated fixture registration link remains unavailable until restart")
	}
	response, err := f.next.RoundTrip(request)
	if err == nil && request.URL.Path == "/nodes/register" && response.StatusCode == http.StatusOK && !f.lost.Swap(true) {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 65537))
		_ = response.Body.Close()
		return nil, errors.New("isolated fixture discarded committed registration response")
	}
	return response, err
}

func (f *ownerChainLostReply) CloseIdleConnections() {
	if idle, ok := f.next.(interface{ CloseIdleConnections() }); ok {
		idle.CloseIdleConnections()
	}
}

func prepareOwnerChainState(t *testing.T, c Config, bootstrap []byte) state.Profile {
	t.Helper()
	prepareRecoveryState(t, c, bootstrap, false)
	backend, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	protector, err := state.NewDPAPI(c.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.New(backend, protector, c.InstallationID, c.MaxStatePlainBytes, c.MaxStateCipherBytes)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Load(budget(t))
	if err != nil {
		t.Fatal(err)
	}
	original := stored.Profiles[c.DefaultProfileID]
	originalAttempt, err := original.Attempt.LoginSucceeded(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	original.Attempt = &originalAttempt
	original.Secrets.Access = strings.Repeat("a", 43)
	original.Secrets.SessionID = "6f632fda-9df6-4a2f-999e-7d778d8ef389"
	original.Secrets.SessionExpiresAt = time.Now().UTC().Add(time.Hour)
	if err := store.Update(budget(t), stored.Revision, func(next *state.State) error {
		next.Profiles[c.DefaultProfileID] = original
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return original
}

func prepareOwnerChainProfile(t *testing.T, c Config, bootstrap []byte) {
	t.Helper()
	backend, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
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
	announcement, err := control.DecodeAnnouncement(c.ControlOrigin, bootstrap, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(budget(t), 1, func(next *state.State) error {
		next.OwnerSID, next.ActiveProfileID = c.ServiceSID, c.DefaultProfileID
		next.Profiles[c.DefaultProfileID] = state.Profile{ID: c.DefaultProfileID, DisplayName: "Fixture", ControlOrigin: c.ControlOrigin, Trust: state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func completeOwnerChainLogin(t *testing.T, runtime *WindowsRuntime, simulator string) {
	t.Helper()
	host, port, err := net.SplitHostPort(simulator)
	if err != nil || host != "127.0.0.1" || port == "" {
		t.Fatal("browser simulator must be loopback")
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// WSL forwards a newly bound fixture listener to Windows asynchronously.
	// Probe only the controller's read-only health endpoint; never retry login POST.
	available := false
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		probeContext, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		request, _ := http.NewRequestWithContext(probeContext, http.MethodGet, "http://"+simulator+"/healthz", nil)
		probe, err := client.Do(request)
		if err == nil {
			_ = probe.Body.Close()
			available = probe.StatusCode == http.StatusNoContent && probe.Header.Get("X-Endlessnet-Fixture") == "owner-chain-login"
		}
		cancel()
		if available {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !available {
		t.Fatal("owner-chain browser controller unavailable")
	}
	started := false
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		stored, err := runtime.state.Store().Load(budget(t))
		if err != nil {
			t.Fatal("owner-chain login state", err)
		}
		profile := stored.Profiles[runtime.config.DefaultProfileID]
		if profile.Attempt != nil && profile.Attempt.Phase == enrollment.ChoosingContext {
			if !started || profile.Secrets.Access == "" || profile.Secrets.SessionID == "" || profile.Secrets.UserID == "" {
				t.Fatal("owner-chain login did not issue a real native session")
			}
			return
		}
		if !started && profile.Login.VerificationURL != "" {
			body, _ := json.Marshal(map[string]string{"verification_url": profile.Login.VerificationURL})
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+simulator+"/simulate-login", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal("owner-chain browser simulation transport", err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatal("owner-chain browser simulation failed", response.StatusCode)
			}
			started = true
		}
		select {
		case <-deadline.C:
			t.Fatal("owner-chain native login deadline")
		case <-tick.C:
		}
	}
}

// The separate orchestrator supplies a disposable public TLS endpoint. With a
// browser controller this starts at IPC Enroll and exchanges a native session
// through the VK wire fixture; otherwise it uses an already-issued test session.
// Installed services, system trust, production state, live VK and the user's
// browser are never used.
func TestWindowsRuntimeAgainstActualOwnerChain(t *testing.T) {
	path := os.Getenv("ENDLESSNET_CLIENT_OWNER_CHAIN_FIXTURE")
	if testing.Short() || path == "" {
		t.Skip("explicit isolated six-owner chain fixture required")
	}
	root, err := filepath.Abs(os.TempDir())
	if err != nil {
		t.Fatal("fixture directory unavailable")
	}
	absolute, err := filepath.Abs(path)
	if err != nil || !strings.HasPrefix(absolute, root+string(filepath.Separator)) {
		t.Fatal("owner fixture must be in Temp")
	}
	file, err := os.Open(absolute)
	if err != nil {
		t.Fatal("owner fixture unavailable")
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		t.Fatal("owner fixture size")
	}
	var input struct {
		Address          string `json:"address"`
		CA               []byte `json:"ca"`
		Bootstrap        []byte `json:"bootstrap"`
		BrowserSimulator string `json:"browser_simulator,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("owner fixture envelope")
	}
	host, port, err := net.SplitHostPort(input.Address)
	if err != nil || host != "127.0.0.1" || port == "" {
		t.Fatal("owner fixture must be loopback")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(input.CA) {
		t.Fatal("owner fixture CA")
	}
	c := stateFixtureConfig(t)
	c.ControlOrigin = "https://public.invalid"
	c.NativeConsentPrefix = "/api/v1/auth/native-login/"
	var original state.Profile
	if input.BrowserSimulator != "" {
		prepareOwnerChainProfile(t, c, input.Bootstrap)
	} else {
		original = prepareOwnerChainState(t, c, input.Bootstrap)
	}
	var registeredID, credential, registrationRequest string
	var recoveryUntil time.Time
	for cycle := range 2 {
		build, digest := runtimeMetadata(t)
		runtime, err := NewWindows(c, build, digest, io.Discard)
		if err != nil {
			t.Fatal("owner-chain application construction", err)
		}
		observed := runtime.http.Transport.(*observedHTTP)
		transport := observed.base.(*http.Transport)
		transport.Proxy = nil
		transport.TLSClientConfig.RootCAs = roots
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: c.CallTimeout}).DialContext(ctx, "tcp", input.Address)
		}
		lostReply := &ownerChainLostReply{next: transport}
		if cycle == 0 {
			observed.base = lostReply
		}
		if err := runtime.App.Start(budget(t)); err != nil {
			t.Fatal("owner-chain application start", err)
		}
		func() {
			defer func() {
				if err := runtime.App.Stop(budget(t)); err != nil {
					t.Error("owner-chain application stop", err)
				}
			}()
			if cycle == 0 {
				connection, err := localipc.Connect(budget(t), runtime.PipeConfig())
				if err != nil {
					t.Fatal("owner-chain target IPC connect", err)
				}
				defer func() { _ = connection.Close() }()
				client := clientv0.NewClientServiceClient(connection)
				if input.BrowserSimulator != "" {
					before, err := client.GetStatus(budget(t), &clientv0.GetStatusRequest{})
					if err != nil || before.GetStatus().GetMetadata() == nil || before.GetStatus().GetStoredState().GetDeviceFingerprintPresent() {
						t.Fatal("owner-chain profile was not initially unenrolled", err)
					}
					started, err := client.Enroll(budget(t), &clientv0.EnrollRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: runtime.PipeConfig().InstanceID, ExpectedRevision: before.Status.Metadata.Revision}, Mode: clientv0.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE, Hostname: "owner-chain-device", RequestId: "d6d90a7d-8892-4adf-a161-91a000c3a604"})
					if err != nil || started.GetAttempt().GetAttemptId() == "" {
						t.Fatal("owner-chain IPC Enroll", err)
					}
					stored, err := runtime.state.Store().Load(budget(t))
					if err != nil {
						t.Fatal(err)
					}
					original = stored.Profiles[c.DefaultProfileID]
					if original.Attempt == nil || original.Attempt.AttemptID != started.Attempt.AttemptId || len(original.Keys.IdentityPrivate) == 0 {
						t.Fatal("owner-chain Enroll was not durable")
					}
					completeOwnerChainLogin(t, runtime, input.BrowserSimulator)
				}
				targets, err := client.ListEnrollmentTargets(budget(t), &clientv0.ListEnrollmentTargetsRequest{AttemptId: original.Attempt.AttemptID})
				if err != nil || targets == nil || targets.Metadata == nil || len(targets.Targets) != 1 {
					t.Fatal("owner-chain authoritative target list", err)
				}
				selected, err := client.SelectEnrollmentTarget(budget(t), &clientv0.SelectEnrollmentTargetRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: runtime.PipeConfig().InstanceID, ExpectedRevision: targets.Metadata.Revision}, AttemptId: original.Attempt.AttemptID, AccountId: "account", NetworkId: "network"})
				if err != nil || selected == nil || selected.Attempt == nil {
					t.Fatal("owner-chain authoritative target selection", err)
				}
			}
			deadline := time.NewTimer(15 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(20 * time.Millisecond)
			defer tick.Stop()
			for {
				stored, err := runtime.state.Store().Load(budget(t))
				if err != nil {
					t.Fatal("owner-chain protected state read", err)
				}
				profile := stored.Profiles[c.DefaultProfileID]
				if profile.Attempt == nil || !reflect.DeepEqual(profile.Keys, original.Keys) || profile.Attempt.Binding != original.Attempt.Binding || !profile.Attempt.Deadline.Equal(original.Attempt.Deadline) {
					t.Fatal("owner-chain changed original identity or budget")
				}
				if cycle == 0 && profile.Attempt.Uncertain || cycle == 1 && profile.Attempt.Phase == enrollment.Registered {
					if cycle == 0 {
						if !lostReply.lost.Load() || profile.Attempt.NodeID != "" || profile.Secrets.NodeCredential != "" || profile.Secrets.RegistrationRequest == "" || profile.Attempt.RecoveryUntil.IsZero() {
							t.Fatal("owner-chain lost response produced false success or lost recovery")
						}
						registrationRequest, recoveryUntil = profile.Secrets.RegistrationRequest, profile.Attempt.RecoveryUntil
					} else {
						if profile.Attempt.AccountID != "account" || profile.Attempt.NetworkID != "network" || profile.Attempt.NodeID == "" || profile.Secrets.NodeCredential == "" || profile.RemoteInFlight || profile.Attempt.Uncertain || profile.Secrets.RegistrationRequest != registrationRequest || !profile.Attempt.RecoveryUntil.Equal(recoveryUntil) {
							t.Fatal("owner-chain restart changed original request/budget or failed registration")
						}
						registeredID, credential = profile.Attempt.NodeID, profile.Secrets.NodeCredential
						assertOwnerChainNodeList(t, runtime.http, profile)
					}
					connection, err := localipc.Connect(budget(t), runtime.PipeConfig())
					if err != nil {
						t.Fatal("owner-chain IPC connect", err)
					}
					defer func() { _ = connection.Close() }()
					client := clientv0.NewClientServiceClient(connection)
					status, err := client.GetStatus(budget(t), &clientv0.GetStatusRequest{})
					if err != nil || status == nil || status.Status == nil {
						t.Fatal("owner-chain IPC status", err)
					}
					projection := status.Status
					if projection.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED || projection.StoredState == nil || projection.StoredState.NodeCredentialPresent != (cycle == 1) || !projection.StoredState.DeviceFingerprintPresent {
						t.Fatal("owner-chain IPC lost confirmed registration/disconnected state")
					}
					if cycle == 0 && (projection.ControlState == clientv0.ControlState_CONTROL_STATE_REGISTERED || projection.NodeId != "") || cycle == 1 && (projection.ControlState != clientv0.ControlState_CONTROL_STATE_REGISTERED || projection.NodeId != registeredID || projection.AccountId != "account" || projection.Network == nil || projection.Network.Id != "network") {
						t.Fatal("owner-chain IPC promoted uncertainty or lost confirmed context")
					}
					encoded, err := json.Marshal(status)
					if err != nil {
						t.Fatal("owner-chain IPC leaked credential")
					}
					for _, secret := range []string{credential, profile.Secrets.Access, profile.Secrets.Renewal, profile.Secrets.RequestProof, profile.Secrets.PollProof} {
						if secret != "" && bytes.Contains(encoded, []byte(secret)) {
							t.Fatal("owner-chain IPC leaked credential")
						}
					}
					return
				}
				select {
				case <-deadline.C:
					t.Fatal("owner-chain registration deadline", profile.Attempt.Phase, profile.Registration.BlockedReason)
				case <-tick.C:
				}
			}
		}()
		assertPipeClosed(t, runtime.PipeConfig())
	}
}
