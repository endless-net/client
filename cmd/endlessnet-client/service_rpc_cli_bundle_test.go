package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNativeBundleReadValidatesBeforeReturningBytes(t *testing.T) {
	for _, mode := range []string{"valid", "digest", "expired", "lifetime", "oversize", "empty", "offset", "early-eof", "missing-eof", "nil", "failure", "cancel", "expires-during-read"} {
		t.Run(mode, func(t *testing.T) {
			payload := bytes.Repeat([]byte("archive"), 10000)
			digest := sha256.Sum256(payload)
			instant := time.Unix(1800000000, 0)
			now := func() time.Time { return instant }
			bundle := &ipc.BundleResult{BundleId: "opaque", CreatedAt: timestamppb.New(instant), ExpiresAt: timestamppb.New(instant.Add(15 * time.Minute)), SizeBytes: uint64(len(payload)), Sha256: hex.EncodeToString(digest[:])}
			switch mode {
			case "digest":
				bundle.Sha256 = hex.EncodeToString(make([]byte, 32))
			case "expired":
				instant = instant.Add(15 * time.Minute)
			case "lifetime":
				bundle.ExpiresAt = timestamppb.New(instant.Add(time.Hour))
			case "oversize":
				bundle.SizeBytes = 5<<20 + 1
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			result, err := readNativeDiagnosticsBundle(ctx, bundle, now, func(_ context.Context, request *ipc.ReadDiagnosticsBundleRequest) (*ipc.ReadDiagnosticsBundleResponse, error) {
				calls++
				if request.BundleId != "opaque" || request.MaxBytes != 64<<10 {
					t.Fatal("invalid chunk request")
				}
				end := min(request.Offset+uint64(request.MaxBytes), uint64(len(payload)))
				out := &ipc.ReadDiagnosticsBundleResponse{Data: payload[request.Offset:end], NextOffset: end, Eof: end == uint64(len(payload))}
				switch mode {
				case "empty":
					out.Data = nil
				case "offset":
					out.NextOffset++
				case "early-eof":
					out.Eof = true
				case "missing-eof":
					out.Eof = false
				case "nil":
					return nil, nil
				case "failure":
					return nil, errors.New("read failed")
				case "cancel":
					cancel()
				case "expires-during-read":
					instant = instant.Add(15 * time.Minute)
				}
				return out, nil
			})
			if mode == "valid" {
				if err != nil || !bytes.Equal(result, payload) || calls != 2 {
					t.Fatal("valid archive failed", err)
				}
				result[0] ^= 1
				if result[0] == payload[0] {
					t.Fatal("archive aliases transport buffer")
				}
			} else if err == nil || result != nil {
				t.Fatal("invalid archive returned partial data")
			}
			if (mode == "expired" || mode == "lifetime" || mode == "oversize") && calls != 0 {
				t.Fatal("invalid metadata reached transport")
			}
		})
	}
}

func TestNativeBundleExportRequiresOperationAndProfile(t *testing.T) {
	for _, args := range [][]string{nil, {"--operation-id", "not-a-uuid"}, {"--bundle-id", "arbitrary"}, {"--path", "arbitrary"}} {
		var output bytes.Buffer
		if err := cmdServiceRPCBundleExport(args, &output); err == nil || output.Len() != 0 {
			t.Fatal("export accepted missing or arbitrary context")
		}
	}
}
