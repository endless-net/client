package client

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestNativeDiagnosticsArchiveRedactsBeforeEncoding(t *testing.T) {
	source := &ipc.Diagnostics{Truncated: true, Failures: []*ipc.Failure{{Code: ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, ReasonKey: "partial"}},
		Status:     &ipc.Status{PendingAction: &ipc.UserAction{BrowserUrl: "https://approval.test/private-code"}},
		RecentLogs: []*ipc.LogEntry{{Message: "normal message"}, {Message: "PrivateKey = synthetic-private"}, {Message: "session_token=synthetic-session"}},
		Peers:      []*ipc.Peer{{Id: "peer", OverlayAddresses: []string{"100.64.0.1", "node_credential=synthetic-node"}}}}
	unknown := protowire.AppendTag(nil, 999, protowire.BytesType)
	unknown = protowire.AppendString(unknown, "synthetic-unknown")
	source.Status.ProtoReflect().SetUnknown(unknown)
	before := proto.Clone(source)
	data, err := buildNativeDiagnosticsArchive(source)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) != 1 || reader.File[0].Name != "diagnostics.json" {
		t.Fatal("invalid archive", err)
	}
	file, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	raw, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"synthetic-private", "synthetic-session", "synthetic-node", "synthetic-unknown", "private-code"} {
		if bytes.Contains(raw, []byte(value)) {
			t.Fatal("archive retained sensitive fixture")
		}
	}
	result := &ipc.Diagnostics{}
	if err := protojson.Unmarshal(raw, result); err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Failures) != 1 || result.RecentLogs[0].Message != "normal message" || result.Peers[0].OverlayAddresses[0] != "100.64.0.1" || result.Status.PendingAction.BrowserUrl != "" {
		t.Fatal("archive changed public diagnostics meaning")
	}
	if !proto.Equal(source, before) {
		t.Fatal("archive redaction changed live source")
	}
}

func TestNativeDiagnosticsArchiveBounds(t *testing.T) {
	for _, source := range []*ipc.Diagnostics{nil, {OsVersion: strings.Repeat("x", rpcBundleMaxBytes+1)}, {OsVersion: strings.Repeat("\x01", rpcBundleMaxBytes/2)}} {
		if data, err := buildNativeDiagnosticsArchive(source); err == nil || data != nil {
			t.Fatal("invalid or JSON-expanded source archived")
		}
	}
}
