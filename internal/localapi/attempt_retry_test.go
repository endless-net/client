package localapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	signing "github.com/endless-net/signing/api/contract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type observationAnnouncements struct {
	source                 *control.Bootstrap
	remaining, calls, gets int
	deadlines              []time.Time
	failure                error
}

func (s *observationAnnouncements) Fetch(ctx context.Context, origin string) (control.Announcement, error) {
	s.calls++
	deadline, _ := ctx.Deadline()
	s.deadlines = append(s.deadlines, deadline)
	if s.failure != nil {
		return control.Announcement{}, s.failure
	}
	a, err := s.source.Fetch(ctx, origin)
	if err != nil {
		return a, err
	}
	if s.remaining > 0 {
		s.remaining--
		// Model a concurrent verified refresh winning before trust pin CAS.
		// Both checkpoints come from real producer signature verification.
		if _, err = s.source.Fetch(ctx, origin); err != nil {
			return control.Announcement{}, err
		}
	}
	return a, nil
}

func observationSource(t *testing.T, c *Controller, now *time.Time, contentions int) *observationAnnouncements {
	t.Helper()
	origin := c.config.DefaultControlOrigin
	shape, err := c.config.Announcements.Fetch(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	var bundles signing.ServerKeyResponse
	if err = json.Unmarshal(shape.Bundle, &bundles); err != nil {
		t.Fatal(err)
	}
	start, end := now.Add(-time.Hour), now.Add(2*time.Hour)
	for _, bundle := range []*signing.BootstrapTrustBundle{&bundles.TrustBundle, &bundles.NodeCredentialTrustBundle, &bundles.RelayTrustBundle} {
		for i := range bundle.Keys {
			bundle.Keys[i].NotBefore = &start
			bundle.Keys[i].NotAfter = &end
		}
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{92}, 32))
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	root := signing.BootstrapRoot{KeyID: "ed25519:" + base64.RawURLEncoding.EncodeToString(digest[:]), PublicKey: public}
	s := &observationAnnouncements{remaining: contentions}
	s.source, err = control.NewVerifiedBootstrap(&http.Client{Transport: bootstrapTransport(func(r *http.Request) (*http.Response, error) {
		s.gets++
		if r.Method != http.MethodGet || r.URL.String() != origin+"/server-key" || r.Header.Get("Authorization") != "" {
			t.Fatal("observation attempted a credential-bearing remote mutation")
		}
		metadata := signing.BootstrapMetadata{Origin: origin, Revision: uint64(s.gets), IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Bundles: bundles}
		message, err := signing.BootstrapSigningBytes(metadata, root.KeyID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(signing.SignedBootstrap{Version: 2, Purpose: "bootstrap", RootKeyID: root.KeyID, Metadata: metadata, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, message))})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
	})}, 1<<20, c.config.Now, map[string]signing.BootstrapRoot{origin: root}, c.store)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOriginalObservationRetriesTrustCASWithinOneBudget(t *testing.T) {
	for _, contentions := range []int{1, 2, 3, 5} {
		t.Run(string(rune('0'+contentions)), func(t *testing.T) {
			c, _, runner, now := fixture(t)
			caller := localipc.Principal{SID: "S-1-5-21-11"}
			started, err := c.begin(t.Context(), caller, request(1))
			if err != nil {
				t.Fatal(err)
			}
			before, err := c.store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			original := before.Profiles[defaultID]
			source := observationSource(t, c, now, contentions)
			c.config.Announcements = source
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			got, err := c.get(ctx, caller, getRequest())
			expectedCalls := min(contentions+1, 3)
			if source.calls != expectedCalls {
				t.Fatal("observation did not respect three-read bound", source.calls, err)
			}
			for _, actual := range source.deadlines {
				if !actual.Equal(deadline) {
					t.Fatal("retry reset the original observation budget")
				}
			}
			if contentions < 3 {
				if err != nil || got.GetAttempt().GetRequestId() != requestID || got.Attempt.AttemptId != started.Attempt.AttemptId || runner.calls != 1 || source.gets != 2*contentions+1 {
					t.Fatal("transient trust CAS abandoned original observation", err)
				}
			} else {
				if status.Code(err) != codes.Aborted || got != nil || runner.calls != 0 || source.gets != 6 {
					t.Fatal("persistent contention escaped bound or dispatched work", err)
				}
				details := status.Convert(err).Details()
				if len(details) != 1 {
					t.Fatal("persistent contention lost typed stale details", err)
				}
				failure, ok := details[0].(*clientv0.Failure)
				if len(details) != 1 || !ok || failure.Code != clientv0.ErrorCode_ERROR_CODE_STALE_STATE || failure.ReasonKey != "client.state.stale" {
					t.Fatal("persistent contention lost typed stale result", err)
				}
			}
			after, err := c.store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			current := after.Profiles[defaultID]
			if len(after.Profiles) != 1 || current.Attempt.Binding != original.Attempt.Binding || !bytes.Equal(current.Keys.IdentityPrivate, original.Keys.IdentityPrivate) || !bytes.Equal(current.Keys.WireGuardPrivate, original.Keys.WireGuardPrivate) || current.Secrets != original.Secrets || !current.Attempt.Deadline.Equal(original.Attempt.Deadline) {
				t.Fatal("observation retry changed original identity/inputs/deadline")
			}
		})
	}
}

func TestObservationDependencyAbortedIsNotLocalStaleOrTrust(t *testing.T) {
	c, _, runner, now := fixture(t)
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	if _, err := c.begin(t.Context(), caller, request(1)); err != nil {
		t.Fatal(err)
	}
	source := observationSource(t, c, now, 0)
	source.failure = status.Error(codes.Aborted, "private dependency detail")
	c.config.Announcements = source
	got, err := c.get(t.Context(), caller, getRequest())
	if err != nil || source.calls != 1 || source.gets != 0 || runner.calls != 0 || got.Attempt.BrowserAction != nil || got.Attempt.GetRestriction().GetReasonKey() != "client.server_identity.unavailable" {
		t.Fatal("arbitrary Aborted retried or enlarged trust", err)
	}
	// Public mutation callers still sanitize local CAS rather than leaking a raw
	// Go sentinel across RPC. Only original observation performs bounded rereads.
	source.failure = nil
	source.remaining = 1
	err = c.dispatch(t.Context(), got.Attempt)
	if status.Code(err) != codes.Aborted || errors.Is(err, state.ErrStale) || source.calls != 2 || runner.calls != 0 {
		t.Fatal("public dispatch lost its sanitized boundary", err)
	}
	// Even a dependency with a superficially matching RPC code/message is not
	// the private local-CAS sentinel, and must not cause another worker dispatch.
	runner.err = status.Error(codes.Aborted, "client.state.stale")
	_, err = c.get(t.Context(), caller, getRequest())
	if status.Code(err) != codes.Unavailable || source.calls != 3 || runner.calls != 1 {
		t.Fatal("runner Aborted was treated as retryable local observation", err)
	}
}
