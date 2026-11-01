// Package control implements Client adapters using published producer SDKs.
package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	signingcontract "github.com/endless-net/signing/api/contract"
)

var ErrBootstrap = errors.New("server trust announcement unavailable")

type Announcement struct {
	Origin, KeyID, ID string
	Bundle            []byte
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
}

func NewBootstrap(client *http.Client, maxBytes int64, now func() time.Time) (*Bootstrap, error) {
	if client == nil || client.Jar != nil || maxBytes <= 0 || maxBytes >= int64(^uint(0)>>1) {
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
	if copy.Timeout <= 0 || copy.Timeout > 30*time.Second {
		copy.Timeout = 30 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &Bootstrap{client: &copy, maxBytes: maxBytes, now: now}, nil
}

func (b *Bootstrap) Fetch(ctx context.Context, origin string) (Announcement, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Host != strings.ToLower(u.Host) || u.String() != origin {
		return Announcement{}, ErrBootstrap
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/server-key", nil)
	if err != nil {
		return Announcement{}, ErrBootstrap
	}
	r.Header.Set("Accept", "application/json")
	response, err := b.client.Do(r)
	if err != nil {
		return Announcement{}, ErrBootstrap
	}
	defer func() { _ = response.Body.Close() }()
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || response.StatusCode != http.StatusOK {
		return Announcement{}, ErrBootstrap
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, b.maxBytes+1))
	if err != nil || int64(len(data)) > b.maxBytes {
		return Announcement{}, ErrBootstrap
	}
	if ctx.Err() != nil {
		return Announcement{}, ErrBootstrap
	}
	return DecodeAnnouncement(origin, data, b.now())
}
