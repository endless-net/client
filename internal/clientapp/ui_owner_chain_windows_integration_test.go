//go:build windows && integration

package clientapp

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

// This opt-in composition runs the UI owner's test against an isolated Client.
// Neither UI nor Flutter is a public Client build/unit dependency.
func TestWindowsUIAgainstActualOwnerChain(t *testing.T) {
	fixture := os.Getenv("ENDLESSNET_CLIENT_OWNER_CHAIN_FIXTURE")
	ui := os.Getenv("ENDLESSNET_CLIENT_UI_OWNER_APP")
	flutter := os.Getenv("ENDLESSNET_CLIENT_UI_FLUTTER_ROOT")
	if testing.Short() || fixture == "" || ui == "" || flutter == "" {
		t.Skip("explicit isolated owner chain, UI source and Flutter SDK required")
	}
	absolute, err := filepath.Abs(fixture)
	if err != nil || !strings.HasPrefix(absolute, os.TempDir()+string(filepath.Separator)) {
		t.Fatal("owner fixture must be in Temp")
	}
	info, err := os.Stat(absolute)
	if err != nil || info.Size() > 65536 {
		t.Fatal("owner fixture unavailable")
	}
	raw, err := os.ReadFile(absolute)
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Address          string `json:"address"`
		CA               []byte `json:"ca"`
		Bootstrap        []byte `json:"bootstrap"`
		BrowserSimulator string `json:"browser_simulator"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("owner fixture envelope")
	}
	for _, endpoint := range []string{input.Address, input.BrowserSimulator} {
		host, port, err := net.SplitHostPort(endpoint)
		if err != nil || host != "127.0.0.1" || port == "" {
			t.Fatal("loopback fixture required")
		}
	}
	for _, path := range []string{ui, flutter} {
		if !filepath.IsAbs(path) || len(filepath.VolumeName(path)) != 2 {
			t.Fatal("explicit local fixture tools required")
		}
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(input.CA) {
		t.Fatal("owner fixture CA")
	}
	c := stateFixtureConfig(t)
	c.ControlOrigin, c.NativeConsentPrefix = "https://public.invalid", "/api/v1/auth/native-login/"
	prepareOwnerChainProfile(t, c, input.Bootstrap)
	journal := filepath.Join(stateFixtureConfig(t).StateDirectory, "ui-correlation.bin")
	var original state.Profile
	var requestID, attemptID string
	for cycle := range 2 {
		build, digest := runtimeMetadata(t)
		runtime, err := NewWindows(c, build, digest, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		observed := runtime.http.Transport.(*observedHTTP)
		transport := observed.base.(*http.Transport)
		transport.Proxy, transport.TLSClientConfig.RootCAs = nil, roots
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: c.CallTimeout}).DialContext(ctx, "tcp", input.Address)
		}
		lost := &ownerChainLostReply{next: transport}
		if cycle == 0 {
			observed.base = lost
		}
		if err := runtime.App.Start(budget(t)); err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				if err := runtime.App.Stop(budget(t)); err != nil {
					t.Error(err)
				}
			}()
			directory := t.TempDir()
			resultPath := filepath.Join(directory, "ui-result.json")
			pipe := runtime.PipeConfig()
			parameters := map[string]any{"cycle": cycle, "instance": pipe.InstanceID, "sid": pipe.ServerSID, "digest": hex.EncodeToString(digest[:]), "library": filepath.Join(ui, "build/windows/x64/runner/Release/endlessnet_windows_pipe.dll"), "profile": c.DefaultProfileID, "origin": c.ControlOrigin, "journal": journal, "result": resultPath, "browser_simulator": input.BrowserSimulator}
			encoded, err := json.Marshal(parameters)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "ui-input.json")
			if err := os.WriteFile(path, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			commandContext, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			command := exec.CommandContext(commandContext, filepath.Join(flutter, "bin/cache/dart-sdk/bin/dart.exe"), filepath.Join(flutter, "bin/cache/flutter_tools.snapshot"), "test", "--no-pub", "--reporter", "expanded", "--plain-name", "real Windows Client owner chain", "test/integration/owner_chain_test.dart")
			command.WaitDelay = 3 * time.Second
			command.Dir = ui
			command.Env = append(os.Environ(), "ENDLESSNET_UI_OWNER_CHAIN_FIXTURE="+path, "FLUTTER_ROOT="+flutter, "PUB_CACHE="+filepath.Join(os.Getenv("LOCALAPPDATA"), "Pub/Cache"), "PATH="+strings.Join([]string{filepath.Join(os.Getenv("SystemRoot"), "System32"), os.Getenv("SystemRoot"), "C:/Program Files/Git/cmd", filepath.Join(flutter, "bin"), filepath.Join(flutter, "bin/cache/dart-sdk/bin")}, ";"))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("UI owner-chain process failed: %v; %s", err, output)
			}
			t.Log(string(output))
			raw, err := os.ReadFile(resultPath)
			if err != nil {
				t.Fatal("UI did not confirm result", err)
			}
			var result struct {
				RequestID  string `json:"request_id"`
				AttemptID  string `json:"attempt_id"`
				NodeID     string `json:"node_id"`
				Registered bool   `json:"registered"`
			}
			if json.Unmarshal(raw, &result) != nil {
				t.Fatal("UI result invalid")
			}
			stored, err := runtime.state.Store().Load(budget(t))
			if err != nil {
				t.Fatal(err)
			}
			profile := stored.Profiles[c.DefaultProfileID]
			if profile.Attempt == nil || result.RequestID != profile.Attempt.RequestID || result.AttemptID != profile.Attempt.AttemptID {
				t.Fatal("UI correlation is not authoritative")
			}
			if cycle == 0 {
				if !lost.lost.Load() || !profile.Attempt.Uncertain || result.Registered || result.NodeID != "" || profile.Secrets.NodeCredential != "" {
					t.Fatal("UI falsely confirmed lost response")
				}
				original, requestID, attemptID = profile, result.RequestID, result.AttemptID
			} else if !result.Registered || result.NodeID == "" || result.NodeID != profile.Attempt.NodeID || profile.Attempt.Phase != enrollment.Registered || profile.Secrets.NodeCredential == "" || profile.RemoteInFlight || result.RequestID != requestID || result.AttemptID != attemptID || profile.Attempt.Binding != original.Attempt.Binding || !reflect.DeepEqual(profile.Keys, original.Keys) || !profile.Attempt.Deadline.Equal(original.Attempt.Deadline) || !profile.Attempt.RecoveryUntil.Equal(original.Attempt.RecoveryUntil) || profile.Secrets.RegistrationRequest != original.Secrets.RegistrationRequest {
				t.Fatal("UI reopen changed original registration or recovery scope")
			}
			if cycle == 1 {
				assertOwnerChainNodeList(t, runtime.http, profile)
			}
		}()
		assertPipeClosed(t, runtime.PipeConfig())
	}
}
