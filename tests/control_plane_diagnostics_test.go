package tests

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"runtime"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// HC-053/056/057: only public native CLI responses and downloaded bytes.
// No private config reads, server file paths, mtime-based expiry or directory repair.
func TestControlPlaneDiagnosticsExport(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.NewTLS(t)
	network, token, err := s.AddNetwork("diagnostics", "100.90.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.TrustControlTLS(s)
	n.Enroll(s, network.Name, token)
	n.Start()
	initial := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId != "" && v.ActiveProfileId != "" && v.MapRevision > 0 })
	id, profile := initial.NodeId, initial.ActiveProfileId
	check := func(disconnected bool) *ipc.Diagnostics {
		t.Helper()
		n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.UserDisconnected == disconnected && v.MapRevision > 0
		})
		response := &ipc.GetDiagnosticsResponse{}
		if err := n.NativeService("diagnostics", response, "--profile-id", profile); err != nil {
			t.Fatal(err)
		}
		d := response.Diagnostics
		if d == nil || d.Status.GetNodeId() != id || d.Status.ActiveProfileId != profile || d.Status.UserDisconnected != disconnected || d.OsName != runtime.GOOS || d.GetClient().GetArchitecture() != runtime.GOARCH {
			t.Fatal("diagnostics lost identity, intent or platform")
		}
		if disconnected && d.Status.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_DISCONNECTED {
			t.Fatal("diagnostics lost disconnected intent")
		}
		if d.Truncated && len(d.Failures) == 0 {
			t.Fatal("partial diagnostics omitted failure markers")
		}
		return d
	}
	check(false)
	s.SetUnavailable(true)
	n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.ControlState == ipc.ControlState_CONTROL_STATE_DEGRADED })
	failed := check(false)
	if failed.Status.MapRevision == 0 || failed.Status.NodeId != id {
		t.Fatal("control outage erased cached identity")
	}
	s.SetUnavailable(false)
	n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.ControlState == ipc.ControlState_CONTROL_STATE_READY })
	disconnect := &ipc.DisconnectResponse{}
	status := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.ActiveProfileId == profile })
	if err := n.NativeService("disconnect", disconnect, testclient.NativeMutationArguments("00000000-0000-4000-8000-000000000001", status)...); err != nil {
		t.Fatal(err)
	}
	if n.AwaitNativeOperation(disconnect.GetOperation().GetId()).State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("disconnect failed")
	}
	check(true)
	n.Stop()
	n.Start()
	check(true)
	status = n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.ActiveProfileId == profile })
	args := testclient.NativeMutationArguments("00000000-0000-4000-8000-000000000002", status)
	accepted := &ipc.CreateDiagnosticsBundleResponse{}
	if err := n.NativeService("diagnostics-bundle", accepted, args...); err != nil {
		t.Fatal(err)
	}
	op := n.AwaitNativeOperation(accepted.GetOperation().GetId())
	bundle := op.GetBundle()
	if op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || bundle == nil || bundle.GetCreatedAt().CheckValid() != nil || bundle.GetExpiresAt().CheckValid() != nil || bundle.ExpiresAt.AsTime().Sub(bundle.CreatedAt.AsTime()) != 15*time.Minute || bundle.SizeBytes == 0 || bundle.SizeBytes > 5<<20 {
		t.Fatal("invalid bundle result")
	}
	download := func() []byte {
		t.Helper()
		data, err := n.ServiceCommand("export-diagnostics-bundle", "--operation-id", op.Id, "--profile-id", profile)
		if err != nil {
			t.Fatal("native bundle download failed (output withheld)")
		}
		digest := sha256.Sum256(data)
		if uint64(len(data)) != bundle.SizeBytes || hex.EncodeToString(digest[:]) != bundle.Sha256 {
			t.Fatal("download differs from descriptor")
		}
		return data
	}
	data := download()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) != 1 || archive.File[0].Name != "diagnostics.json" {
		t.Fatal("invalid archive")
	}
	file, err := archive.File[0].Open()
	if err != nil {
		t.Fatal("cannot open diagnostics entry")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 5<<20+1))
	closeErr := file.Close()
	exported := &ipc.Diagnostics{}
	if readErr != nil || closeErr != nil || len(raw) > 5<<20 || protojson.Unmarshal(raw, exported) != nil {
		t.Fatal("export violates native diagnostics schema")
	}
	if exported.GetStatus().GetNodeId() != id || !exported.Status.UserDisconnected {
		t.Fatal("export lost identity or disconnected intent")
	}
	n.Crash()
	n.Start()
	check(true)
	repeated := &ipc.CreateDiagnosticsBundleResponse{}
	if err := n.NativeService("diagnostics-bundle", repeated, args...); err != nil || !proto.Equal(repeated.Operation, op) {
		t.Fatal("exact retry changed operation after crash")
	}
	if !bytes.Equal(download(), data) {
		t.Fatal("archive changed across restart")
	}
	connected := &ipc.ConnectResponse{}
	status = n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.ActiveProfileId == profile })
	if err := n.NativeService("connect", connected, testclient.NativeMutationArguments("00000000-0000-4000-8000-000000000003", status)...); err != nil {
		t.Fatal(err)
	}
	if n.AwaitNativeOperation(connected.GetOperation().GetId()).State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("connect failed")
	}
	check(false)
}
