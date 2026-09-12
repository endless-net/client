package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-053/HC-056/HC-057: inspect the public diagnostic response and exported
// artifact, never the agent's config, snapshot or credential files.
func TestControlPlaneDiagnosticsExport(t *testing.T) {
	s, n, id := controlScenario(t)
	output, err := n.ServiceCommand("diagnostics-bundle")
	if err == nil || !strings.Contains(string(output), "diagnostics bundle directory is not configured") {
		t.Fatal("unconfigured diagnostic export did not report its public error")
	}
	n.Stop()
	directory := filepath.Join(filepath.Dir(n.Config), "diagnostic-export")
	n.AgentArgs = append(n.AgentArgs, "--diagnostics-dir", directory)
	n.Start()
	check := func(disconnected bool) ipc.Diagnostics {
		t.Helper()
		before := n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == id && v.CachedMapValid && v.UserDisconnected == disconnected
		})
		var response ipc.DiagnosticsResponse
		n.Service("diagnostics", &response)
		d := response.Diagnostics
		if d.Status.NodeID != id || d.Status.NetworkID != before.NetworkID || d.Status.DesiredState != before.DesiredState || d.Status.UserDisconnected != disconnected || !d.Status.CachedMapValid {
			t.Fatal("diagnostic status disagreed with public identity or connection intent")
		}
		if disconnected && d.Status.State != ipc.StateDisconnected {
			t.Fatal("diagnostics misreported user disconnection as a failure")
		}
		if d.Runtime.GOOS != runtime.GOOS || d.Runtime.GOARCH != runtime.GOARCH || d.Config.NodeID != id || !d.Config.NodeCredentialPresent || !d.Config.IdentityPrivateKeyPresent || !d.Config.PrivateKeyPresent {
			t.Fatal("diagnostics omitted platform, identity or credential presence metadata")
		}
		return d
	}
	check(false)
	s.SetUnavailable(true)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.State == ipc.StateDegraded && v.NodeID == id && v.CachedMapValid && v.NodeCredentialPresent && v.Agent != nil && v.Agent.LastError != ""
	})
	var controlFailure ipc.DiagnosticsResponse
	n.Service("diagnostics", &controlFailure)
	failed := controlFailure.Diagnostics.Status
	if failed.State != ipc.StateDegraded || failed.UserDisconnected || failed.NodeID != id || !failed.CachedMapValid || !failed.NodeCredentialPresent || failed.Agent == nil || failed.Agent.LastError == "" {
		t.Fatal("diagnostics did not isolate a control failure from intent, identity and cached access")
	}
	s.SetUnavailable(false)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.State == ipc.StateConnected && v.NodeID == id && v.CachedMapValid && v.NodeCredentialPresent && v.Agent != nil && v.Agent.LastError == ""
	})
	check(false)
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	check(true)
	n.Stop()
	n.Start()
	check(true)
	var bundle ipc.DiagnosticsBundleResponse
	n.Service("diagnostics-bundle", &bundle)
	relative, err := filepath.Rel(directory, bundle.Path)
	if err != nil || !filepath.IsAbs(bundle.Path) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		t.Fatal("diagnostic export escaped its configured output directory")
	}
	created, createdErr := time.Parse(time.RFC3339Nano, bundle.CreatedAt)
	expires, expiresErr := time.Parse(time.RFC3339Nano, bundle.ExpiresAt)
	if createdErr != nil || expiresErr != nil || !expires.After(created) || bundle.SizeBytes <= 0 || bundle.Reused {
		t.Fatal("new diagnostic export has invalid lifecycle metadata")
	}
	data, err := os.ReadFile(bundle.Path)
	if err != nil || int64(len(data)) != bundle.SizeBytes {
		t.Fatal("public diagnostic export is missing or has incorrect size")
	}
	// The published diagnostic schema exposes credential presence flags, not
	// secret fields. Reject additional serialized fields without printing them.
	var exported ipc.Diagnostics
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&exported); err != nil {
		t.Fatal("diagnostic export does not match the published schema")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatal("diagnostic export contains trailing data")
	}
	if exported.Status.NodeID != id || !exported.Status.UserDisconnected || exported.Status.DesiredState != ipc.DesiredDisconnected || exported.Status.State != ipc.StateDisconnected {
		t.Fatal("exported diagnostic status lost disconnected identity or intent")
	}
	n.Crash()
	n.Start()
	check(true)
	var repeated ipc.DiagnosticsBundleResponse
	n.Service("diagnostics-bundle", &repeated)
	if !repeated.Reused || repeated.Path != bundle.Path || repeated.CreatedAt != bundle.CreatedAt || repeated.ExpiresAt != bundle.ExpiresAt || repeated.SizeBytes != bundle.SizeBytes {
		t.Fatal("agent crash recovery did not identify the reusable diagnostic artifact")
	}
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	check(false)
	// Age only the public export, using the lifetime advertised by the CLI.
	// This exercises OS file retention without reading private client state or
	// advancing a test clock inside the running agent.
	note := filepath.Join(directory, "operator-note.txt")
	const noteText = "operator-owned diagnostic note"
	if err := os.WriteFile(note, []byte(noteText), 0o600); err != nil {
		t.Fatal("could not create unrelated test note")
	}
	aged := time.Now().Add(-expires.Sub(created) - time.Hour)
	if err := os.Chtimes(bundle.Path, aged, aged); err != nil {
		t.Fatal("could not age the public diagnostic artifact")
	}
	var renewed ipc.DiagnosticsBundleResponse
	n.Service("diagnostics-bundle", &renewed)
	if renewed.Reused || renewed.Path == bundle.Path || filepath.Dir(renewed.Path) != directory || renewed.SizeBytes <= 0 {
		t.Fatal("retention did not produce a new export within the configured directory")
	}
	if _, err := os.Stat(bundle.Path); !os.IsNotExist(err) {
		t.Fatal("expired public diagnostic artifact was retained")
	}
	if data, err := os.ReadFile(note); err != nil || string(data) != noteText {
		t.Fatal("diagnostic retention changed an unrelated operator file")
	}
	newData, err := os.ReadFile(renewed.Path)
	var newExport ipc.Diagnostics
	if err != nil || int64(len(newData)) != renewed.SizeBytes || json.Unmarshal(newData, &newExport) != nil || newExport.Status.NodeID != id || newExport.Status.UserDisconnected || newExport.Status.DesiredState != ipc.DesiredConnected {
		t.Fatal("replacement diagnostic export did not capture current connected intent")
	}
}
