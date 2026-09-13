package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestRecoveryInspectionDoesNotPersist(t *testing.T) {
	for _, scenario := range []string{"success", "terminal", "retryable", "wrong origin", "wrong key", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			var fixture recoveryTestFixture
			var calls atomic.Int32
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if scenario == "cancel" {
					_, _ = io.Copy(io.Discard, r.Body)
					close(started)
					<-r.Context().Done()
					return
				}
				if r.URL.Path == "/server-key" {
					_ = json.NewEncoder(w).Encode(testServerKeyResponseFromBundle(fixture.NewTrust))
					return
				}
				switch scenario {
				case "terminal":
					writeRecoveryPublicError(t, w, clientapi.ErrorCodeNodeCredentialRevoked, "request-terminal")
				case "retryable":
					writeRecoveryPublicError(t, w, clientapi.ErrorCodeTemporarilyUnavailable, "request-retryable")
				default:
					req, err := clientapi.DecodeRegisterNodeRequest(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(recoverySuccessResponse(t, fixture.NewSigningKey, req))
				}
			}))
			defer server.Close()
			fixture = newRecoveryTestFixture(t, server.URL)
			cfg, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			before, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "wrong origin" {
				cfg.EnrollmentRecovery.ConfirmedControlOrigin = "https://different.example.test"
			}
			if scenario == "wrong key" {
				cfg.EnrollmentRecovery.ConfirmedKeyID = "different-key"
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancel" {
				var timeoutCancel context.CancelFunc
				ctx, timeoutCancel = context.WithTimeout(ctx, 2*time.Second)
				defer timeoutCancel()
				go func() {
					select {
					case <-started:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			result, err := inspectEnrollmentRecovery(ctx, cfg)
			switch scenario {
			case "success":
				if err != nil || !result.Progress.Completed || result.Progress.Terminal || result.NetworkMap == nil {
					t.Fatal("validated recovery missing", err)
				}
			case "terminal":
				if err != nil || !result.Progress.Terminal || !result.Progress.Completed || result.Progress.RequestID != "request-terminal" || result.NetworkMap != nil {
					t.Fatal("terminal recovery lost", err)
				}
			case "retryable":
				if err != nil || !result.Progress.Retryable || result.Progress.Completed || result.NetworkMap != nil {
					t.Fatal("retry classification lost", err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || result.NetworkMap != nil {
					t.Fatal("canceled recovery produced outcome", err)
				}
			default:
				if err == nil || calls.Load() != 0 || result.NetworkMap != nil {
					t.Fatal("stale confirmation sent credentials")
				}
			}
			after, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("inspection changed durable registration")
			}
			if cfg.NodeCredential != before.NodeCredential || cfg.MapRevision != before.MapRevision {
				t.Fatal("inspection mutated input")
			}
		})
	}
}
