package client

import (
	"bytes"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCBundleStoreBindingExpiryAndOwnedChunks(t *testing.T) {
	now := time.Unix(1800000000, 0)
	store := &clientRPCBundleStore{now: func() time.Time { return now }}
	data := []byte("archive")
	metadata, err := store.put("owner", "profile", data)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'
	request := &ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId, MaxBytes: 3}
	metadata.ExpiresAt.Seconds++
	first, err := store.read("OWNER", "profile", request)
	if err != nil || string(first.Data) != "arc" || first.NextOffset != 3 || first.Eof {
		t.Fatal("invalid first chunk", err)
	}
	first.Data[0] = 'X'
	again, _ := store.read("owner", "profile", request)
	if string(again.Data) != "arc" {
		t.Fatal("caller changed stored bytes")
	}
	for _, binding := range [][2]string{{"other", "profile"}, {"owner", "other"}} {
		_, err := store.read(binding[0], binding[1], request)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	request.Offset, request.MaxBytes = 3, 0
	last, err := store.read("owner", "profile", request)
	if err != nil || string(last.Data) != "hive" || !last.Eof || last.NextOffset != 7 {
		t.Fatal("invalid final chunk", err)
	}
	request.Offset = 8
	_, err = store.read("owner", "profile", request)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	request.Offset = 0
	now = now.Add(15 * time.Minute)
	_, err = store.read("owner", "profile", request)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	if store.bytes != 0 || len(store.items) != 0 {
		t.Fatal("expired data retained")
	}
}

func TestRPCBundleStoreCapacityAndRevocation(t *testing.T) {
	store := &clientRPCBundleStore{now: time.Now}
	for range 4 {
		if _, err := store.put("owner", "profile", bytes.Repeat([]byte{1}, rpcBundleMaxBytes)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := store.put("owner", "profile", []byte{1})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	store.revoke("other", "profile")
	if store.bytes != rpcBundleStoreMaxBytes {
		t.Fatal("foreign revocation changed store")
	}
	store.revoke("owner", "profile")
	if store.bytes != 0 {
		t.Fatal("revocation retained bytes")
	}
	for range 32 {
		if _, err := store.put("owner", "profile", []byte{1}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = store.put("owner", "profile", []byte{1})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
}

func TestRPCBundleStoreReadBoundsAndRevokedHandles(t *testing.T) {
	store := &clientRPCBundleStore{}
	metadata, err := store.put("owner", "profile", bytes.Repeat([]byte{1}, 300<<10))
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []uint32{0, 1, 256 << 10} {
		chunk, err := store.read("owner", "profile", &ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId, MaxBytes: size})
		want := int(size)
		if size == 0 {
			want = 64 << 10
		}
		if err != nil || len(chunk.Data) != want || chunk.NextOffset != uint64(want) || chunk.Eof {
			t.Fatal("chunk size bound not honored", err)
		}
	}
	end, err := store.read("owner", "profile", &ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId, Offset: metadata.SizeBytes})
	if err != nil || len(end.Data) != 0 || !end.Eof || end.NextOffset != metadata.SizeBytes {
		t.Fatal("exact EOF invalid", err)
	}
	for _, request := range []*ipc.ReadDiagnosticsBundleRequest{nil, {BundleId: "../file"}, {BundleId: metadata.BundleId, MaxBytes: 256<<10 + 1}, {BundleId: metadata.BundleId, Offset: ^uint64(0)}} {
		_, err := store.read("owner", "profile", request)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	other, err := store.put("owner", "other-profile", []byte("other"))
	if err != nil {
		t.Fatal(err)
	}
	store.revoke("owner", "profile")
	_, err = store.read("owner", "profile", &ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	if _, err := store.read("owner", "other-profile", &ipc.ReadDiagnosticsBundleRequest{BundleId: other.BundleId}); err != nil {
		t.Fatal("revocation crossed profile boundary", err)
	}
}

func TestRPCBundleStoreRejectsUnrepresentableClock(t *testing.T) {
	for _, now := range []time.Time{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(9999, 12, 31, 23, 55, 0, 0, time.UTC)} {
		store := &clientRPCBundleStore{now: func() time.Time { return now }}
		_, err := store.put("owner", "profile", []byte("data"))
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		if store.bytes != 0 || len(store.items) != 0 {
			t.Fatal("invalid timestamp persisted bundle")
		}
	}
}
