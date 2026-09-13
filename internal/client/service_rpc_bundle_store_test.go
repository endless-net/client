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
