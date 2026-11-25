// Package control implements Client adapters using published producer SDKs.
package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/state"
	signingcontract "github.com/endless-net/signing/api/contract"
	signingapi "github.com/endless-net/signing/api/v1"
	"github.com/endless-net/signing/api/v1/signingapiconnect"
)

var (
	ErrBootstrap         = errors.New("server trust announcement rejected")
	ErrBootstrapProtocol = errors.New("bootstrap protocol or configuration rejected")
	ErrBootstrapState    = errors.New("local bootstrap checkpoint unavailable")
)

// Still classifies as ErrBootstrap for existing callers. Renewal can distinguish
// a transport outage from rejected signed trust without accepting either as trust.
var ErrBootstrapUnavailable = errors.Join(ErrBootstrap, errors.New("bootstrap transport unavailable"))

type Announcement struct {
	Origin, KeyID, ID           string
	Bundle                      []byte
	verifiedID, verifiedOrigin  string
	verifiedFrom, verifiedUntil time.Time
	checkpoint                  state.BootstrapCheckpoint
}
type Announcements interface {
	Fetch(context.Context, string) (Announcement, error)
}

// ID binds origin and the complete producer projection, including all purpose
// keys/validity/provider versions. Receiving it never establishes trust.
func DecodeAnnouncement(origin string, data []byte, now time.Time) (Announcement, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Host != strings.ToLower(u.Host) || u.String() != origin {
		return Announcement{}, ErrBootstrap
	}
	var value signingcontract.ServerKeyResponse
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil || signingcontract.ValidateServerKeyResponse(value) != nil {
		return Announcement{}, ErrBootstrap
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Announcement{}, ErrBootstrap
	}
	for _, bundle := range []signingcontract.BootstrapTrustBundle{value.TrustBundle, value.NodeCredentialTrustBundle, value.RelayTrustBundle} {
		if _, err = signingcontract.ResolveBootstrapKey(bundle, bundle.ActiveKeyID, now); err != nil {
			return Announcement{}, ErrBootstrap
		}
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return Announcement{}, ErrBootstrap
	}
	digest := sha256.Sum256(append([]byte(origin+"\x00"), canonical...))
	return Announcement{Origin: origin, KeyID: value.TrustBundle.ActiveKeyID, ID: "sha256:" + hex.EncodeToString(digest[:]), Bundle: canonical}, nil
}

func (a Announcement) Validate(now time.Time) error {
	actual, err := DecodeAnnouncement(a.Origin, a.Bundle, now)
	if err != nil || actual.KeyID != a.KeyID || actual.ID != a.ID {
		return ErrBootstrap
	}
	return nil
}

type Bootstrap struct {
	client   *http.Client
	maxBytes int64
	now      func() time.Time
	roots    map[string]signingcontract.BootstrapRoot
	store    *state.Store
}

// RestrictedHTTPClient shares the public daemon HTTP policy. It rejects ambient
// browser/workload credentials and unsafe TLS, disables redirects and bounds the
// complete call. It starts no network operation and never adds request headers.
func RestrictedHTTPClient(client *http.Client, maximum time.Duration) (*http.Client, error) {
	if client == nil || client.Jar != nil || maximum < time.Millisecond || maximum > 5*time.Minute {
		return nil, ErrBootstrap
	}
	copy := *client
	if copy.Transport == nil {
		copy.Transport = http.DefaultTransport
	}
	if transport, ok := copy.Transport.(*http.Transport); ok {
		transport = transport.Clone()
		if cfg := transport.TLSClientConfig; cfg != nil && (cfg.InsecureSkipVerify || len(cfg.Certificates) != 0 || cfg.GetClientCertificate != nil) {
			return nil, ErrBootstrap
		}
		copy.Transport = transport
	}
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrBootstrap }
	if copy.Timeout <= 0 || copy.Timeout > maximum {
		copy.Timeout = maximum
	}
	return &copy, nil
}

func NewBootstrap(client *http.Client, maxBytes int64, now func() time.Time) (*Bootstrap, error) {
	if maxBytes <= 0 || maxBytes >= int64(^uint(0)>>1) {
		return nil, ErrBootstrap
	}
	client, err := RestrictedHTTPClient(client, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &Bootstrap{client: client, maxBytes: maxBytes, now: now}, nil
}

func (b *Bootstrap) Fetch(ctx context.Context, origin string) (Announcement, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Host != strings.ToLower(u.Host) || u.String() != origin {
		return Announcement{}, ErrBootstrap
	}
	root, known := b.roots[origin]
	if !known || b.store == nil {
		return Announcement{}, ErrBootstrap
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := signingapiconnect.NewBootstrapServiceClient(b.client, origin,
		connect.WithReadMaxBytes(int(b.maxBytes)), connect.WithSendMaxBytes(1024))
	response, err := client.GetBootstrap(ctx, connect.NewRequest(&signingapi.GetBootstrapRequest{}))
	if ctx.Err() != nil {
		return Announcement{}, ctx.Err()
	}
	if err != nil {
		return Announcement{}, bootstrapResponseError(err)
	}
	if response == nil || response.Msg == nil {
		return Announcement{}, ErrBootstrapProtocol
	}
	var accepted Announcement
	err = b.store.AcceptBootstrap(ctx, origin, func(previous state.BootstrapCheckpoint) (state.BootstrapCheckpoint, error) {
		metadata, checkpoint, verifyErr := signingcontract.VerifyBootstrapResponse(response.Msg, root, origin, b.now(), signingcontract.BootstrapCheckpoint{Revision: previous.Revision, Digest: previous.Digest})
		if verifyErr != nil {
			return state.BootstrapCheckpoint{}, ErrBootstrap
		}
		bundle, marshalErr := json.Marshal(metadata.Bundles)
		if marshalErr != nil {
			return state.BootstrapCheckpoint{}, ErrBootstrap
		}
		accepted, verifyErr = DecodeAnnouncement(origin, bundle, b.now())
		if verifyErr != nil {
			return state.BootstrapCheckpoint{}, ErrBootstrap
		}
		accepted.verifiedID, accepted.verifiedOrigin, accepted.verifiedFrom, accepted.verifiedUntil = accepted.ID, origin, metadata.IssuedAt, metadata.ExpiresAt
		accepted.checkpoint = state.BootstrapCheckpoint{Revision: checkpoint.Revision, Digest: checkpoint.Digest}
		return accepted.checkpoint, nil
	})
	if ctx.Err() != nil {
		return Announcement{}, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, ErrBootstrap) {
			return Announcement{}, ErrBootstrap
		}
		return Announcement{}, fmt.Errorf("%w: %w", ErrBootstrapState, err)
	}
	if !accepted.VerifiedAt(b.now()) {
		return Announcement{}, ErrBootstrap
	}
	return accepted, nil
}

// RejectedControlTransport identifies TLS trust failures and refused redirects.
func RejectedControlTransport(err error) bool {
	var verification *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	return errors.Is(err, ErrBootstrap) || errors.As(err, &verification) || errors.As(err, &authority) ||
		errors.As(err, &invalid) || errors.As(err, &hostname)
}

func bootstrapResponseError(err error) error {
	if RejectedControlTransport(err) {
		return ErrBootstrap
	}
	var transport *url.Error
	code := connect.CodeOf(err)
	switch code {
	case connect.CodeFailedPrecondition, connect.CodeUnimplemented, connect.CodeDataLoss:
		return ErrBootstrapProtocol
	}
	if errors.As(err, &transport) || code == connect.CodeUnavailable || code == connect.CodeDeadlineExceeded ||
		code == connect.CodeResourceExhausted && connect.IsWireError(err) {
		return ErrBootstrapUnavailable
	}
	return ErrBootstrap
}

// NewVerifiedBootstrap injects independently provisioned public roots and the
// protected installation store. Roots are copied; HTTP cannot add/replace them.
func NewVerifiedBootstrap(client *http.Client, maxBytes int64, now func() time.Time, roots map[string]signingcontract.BootstrapRoot, store *state.Store) (*Bootstrap, error) {
	b, err := NewBootstrap(client, maxBytes, now)
	if err != nil || store == nil {
		return nil, ErrBootstrap
	}
	b.maxBytes = min(b.maxBytes, signingcontract.BootstrapMaxBytes)
	b.store = store
	b.roots = make(map[string]signingcontract.BootstrapRoot, len(roots))
	for origin, root := range roots {
		root.PublicKey = bytes.Clone(root.PublicKey)
		b.roots[origin] = root
	}
	return b, nil
}

// VerifiedAt is proof of this source's successful root verification and durable
// checkpoint acceptance. Shape decoding or caller-populated fields cannot set it.
func (a Announcement) VerifiedAt(now time.Time) bool {
	return a.verifiedID != "" && a.ID == a.verifiedID && a.Origin == a.verifiedOrigin && !now.Before(a.verifiedFrom) && now.Before(a.verifiedUntil) && a.Validate(now) == nil
}

func (a Announcement) Checkpoint() state.BootstrapCheckpoint { return a.checkpoint }

// MatchesProfile permits a new independently verified revision without locking
// consumers to a prior bundle. Injected non-HTTP fixtures retain exact matching;
// production HTTP Bootstrap never returns an unverified announcement.
func (a Announcement) MatchesProfile(p state.Profile, now time.Time) bool {
	if a.verifiedID != "" {
		return a.Origin == p.ControlOrigin && a.VerifiedAt(now)
	}
	return a.Origin == p.ControlOrigin && a.Validate(now) == nil && (a.KeyID == p.Trust.KeyID && a.ID == p.Trust.AnnouncementID && bytes.Equal(a.Bundle, p.Trust.Bundle))
}
