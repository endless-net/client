package client

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCBundleFileRestartRevocationAndExpiry(t *testing.T) {
	now := time.Unix(1800000000, 0)
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "bundles.state")
	store, err := openClientRPCBundleStore(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.put("owner", "profile", []byte("archive"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.put("owner", "other", []byte("other archive"))
	if err != nil {
		t.Fatal(err)
	}
	store, err = openClientRPCBundleStore(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(store.items[metadata.BundleId].metadata, metadata) {
		t.Fatal("descriptor changed on restart")
	}
	chunk, err := store.read("OWNER", "profile", &ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId})
	if err != nil || string(chunk.GetData()) != "archive" {
		t.Fatal("archive lost on restart", err)
	}
	if err := store.revoke("OWNER", "profile"); err != nil {
		t.Fatal(err)
	}
	store, err = openClientRPCBundleStore(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.items[metadata.BundleId]; ok {
		t.Fatal("revoked archive resurrected")
	}
	if _, ok := store.items[other.BundleId]; !ok {
		t.Fatal("unrelated profile lost")
	}
	now = now.Add(15 * time.Minute)
	store, err = openClientRPCBundleStore(path, clock)
	if err != nil || len(store.items) != 0 || store.bytes != 0 {
		t.Fatal("expired archive restored", err)
	}
}

func TestRPCBundlePersistenceFailureIsAtomic(t *testing.T) {
	store := &clientRPCBundleStore{}
	metadata, err := store.put("owner", "profile", []byte("archive"))
	if err != nil {
		t.Fatal(err)
	}
	store.persist = func(map[string]clientRPCBundleRecord) error { return errors.New("synthetic storage failure") }
	result, err := store.put("owner", "profile", []byte("second"))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	if result != nil || len(store.items) != 1 || store.bytes != 7 {
		t.Fatal("failed write published archive")
	}
	assertRPCFailure(t, store.revoke("owner", "profile"), ipc.ErrorCode_ERROR_CODE_INTERNAL)
	chunk, err := store.read("owner", "profile", &ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId})
	if err != nil || string(chunk.GetData()) != "archive" {
		t.Fatal("failed revoke changed live state", err)
	}
}

func TestRPCBundleFileRejectsCorruption(t *testing.T) {
	for _, raw := range []string{`not json`, `[{"owner":"private-value","unexpected":true}]`, `[] {}`, `[{}]`} {
		path := filepath.Join(t.TempDir(), "bundles.state")
		protected, err := protectConfigState([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteFileAtomic(path, protected, 0o600); err != nil {
			t.Fatal(err)
		}
		store, err := openClientRPCBundleStore(path, nil)
		if err == nil || store != nil {
			t.Fatal("corrupt store accepted")
		}
		if err.Error() != "invalid bundle state" {
			t.Fatal("storage error exposed content", err)
		}
	}
}

func TestRPCBundleFileRejectsInvalidRecords(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, test := range []struct {
		name   string
		change func(*ipc.BundleResult, *clientRPCBundleDiskRecord)
	}{
		{"digest", func(m *ipc.BundleResult, _ *clientRPCBundleDiskRecord) { m.Sha256 = "bad" }},
		{"size", func(m *ipc.BundleResult, _ *clientRPCBundleDiskRecord) { m.SizeBytes++ }},
		{"id", func(m *ipc.BundleResult, _ *clientRPCBundleDiskRecord) { m.BundleId = "../archive" }},
		{"missing time", func(m *ipc.BundleResult, _ *clientRPCBundleDiskRecord) { m.CreatedAt = nil }},
		{"ttl", func(m *ipc.BundleResult, _ *clientRPCBundleDiskRecord) {
			m.ExpiresAt = timestamppb.New(now.Add(time.Hour))
		}},
		{"owner", func(_ *ipc.BundleResult, r *clientRPCBundleDiskRecord) { r.Owner = "" }},
		{"profile", func(_ *ipc.BundleResult, r *clientRPCBundleDiskRecord) { r.Profile = "" }},
		{"bytes", func(_ *ipc.BundleResult, r *clientRPCBundleDiskRecord) { r.Data[0] = 'X' }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &clientRPCBundleStore{now: func() time.Time { return now }}
			metadata, err := s.put("owner", "profile", []byte("archive"))
			if err != nil {
				t.Fatal(err)
			}
			record := clientRPCBundleDiskRecord{Owner: "owner", Profile: "profile", Data: []byte("archive")}
			test.change(metadata, &record)
			record.Metadata, err = proto.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal([]clientRPCBundleDiskRecord{record})
			if err != nil {
				t.Fatal(err)
			}
			raw, err = protectConfigState(raw)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "bundles.state")
			if err := WriteFileAtomic(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if store, err := openClientRPCBundleStore(path, s.now); err == nil || store != nil {
				t.Fatal("invalid record restored")
			}
		})
	}
}
