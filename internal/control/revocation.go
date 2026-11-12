package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	contract "github.com/endless-net/coordinator/api/enrollment"
)

var ErrRevocationUnknown = errors.New("remote revocation outcome unknown")
var ErrRevocationConflict = errors.New("remote revocation command conflicts")

type RevocationConfig struct {
	MaxBytes, MaxTargets int
	Now                  func() time.Time
}
type Revocations struct {
	http          *http.Client
	announcements Announcements
	config        RevocationConfig
}

func NewRevocations(client *http.Client, announcements Announcements, c RevocationConfig) (*Revocations, error) {
	if announcements == nil || c.Now == nil || c.MaxBytes < 1 || c.MaxTargets < 1 {
		return nil, ErrTargetProtocol
	}
	c.MaxBytes = min(c.MaxBytes, 65536)
	b, err := NewBootstrap(client, int64(c.MaxBytes), c.Now)
	if err != nil {
		return nil, err
	}
	return &Revocations{http: b.client, announcements: announcements, config: c}, nil
}

func (r *Revocations) call(ctx context.Context, p state.Profile, method, path string, body []byte) ([]byte, http.Header, error) {
	if p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Secrets.Access == "" || p.Secrets.UserID == "" || !r.config.Now().Before(p.Secrets.SessionExpiresAt) {
		return nil, nil, ErrSessionDenied
	}
	ctx, cancel := context.WithTimeout(ctx, min(10*time.Second, p.Secrets.SessionExpiresAt.Sub(r.config.Now())))
	defer cancel()
	a, err := r.announcements.Fetch(ctx, p.ControlOrigin)
	if err != nil || !a.MatchesProfile(p, r.config.Now()) {
		return nil, nil, ErrBootstrap
	}
	if len(body) > r.config.MaxBytes || ctx.Err() != nil {
		return nil, nil, ErrRevocationUnknown
	}
	request, err := http.NewRequestWithContext(ctx, method, p.ControlOrigin+path, nil)
	if err != nil {
		return nil, nil, ErrTargetProtocol
	}
	// A non-replayable body and no GetBody prevent Transport from replaying a
	// mutation after a lost response; no idempotency header authorizes a retry.
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.GetBody = nil
	request.Header.Set("Authorization", "Bearer "+p.Secrets.Access)
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := r.http.Do(request)
	if err != nil {
		return nil, nil, ErrRevocationUnknown
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, nil, ErrSessionDenied
	case http.StatusForbidden:
		return nil, nil, ErrTargetDenied
	case http.StatusConflict:
		return nil, nil, ErrRevocationConflict
	default:
		return nil, nil, ErrRevocationUnknown
	}
	kind, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		return nil, nil, ErrTargetProtocol
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(r.config.MaxBytes)+1))
	if err != nil || len(raw) > r.config.MaxBytes || ctx.Err() != nil || !r.config.Now().Before(p.Secrets.SessionExpiresAt) {
		return nil, nil, ErrRevocationUnknown
	}
	return raw, response.Header.Clone(), nil
}

func (r *Revocations) List(ctx context.Context, p state.Profile) ([]*clientv0.NodeRevocationTarget, error) {
	if p.Attempt == nil {
		return nil, ErrSessionDenied
	}
	shape := contract.RevokeNodeRequest{AccountID: p.Attempt.AccountID, NetworkID: p.Attempt.NetworkID, NodeID: "selection", CommandID: "selection", ExpectedRevision: 1}
	if shape.Validate() != nil {
		return nil, ErrTargetProtocol
	}
	raw, headers, err := r.call(ctx, p, http.MethodGet, "/networks/"+url.PathEscape(shape.NetworkID)+"/nodes", nil)
	if err != nil {
		return nil, err
	}
	var values []string
	for name, entries := range headers {
		if strings.EqualFold(name, contract.NodeListRevisionHeader) {
			values = append(values, entries...)
		}
	}
	if len(values) != 1 {
		return nil, ErrTargetProtocol
	}
	revision, err := contract.ParseNodeListRevision(values[0])
	if err != nil {
		return nil, ErrTargetProtocol
	}
	var nodes []contract.Node
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&nodes) != nil || d.Decode(new(any)) != io.EOF || nodes == nil || len(nodes) > r.config.MaxTargets {
		return nil, ErrTargetProtocol
	}
	seen := make(map[string]bool, len(nodes))
	targets := make([]*clientv0.NodeRevocationTarget, 0, len(nodes))
	for _, n := range nodes {
		shape.NodeID = n.ID
		if shape.Validate() != nil || n.NetworkID != shape.NetworkID || seen[n.ID] {
			return nil, ErrTargetProtocol
		}
		seen[n.ID] = true
		targets = append(targets, &clientv0.NodeRevocationTarget{AccountId: shape.AccountID, NetworkId: shape.NetworkID, NodeId: n.ID, Hostname: n.Hostname, AssignedIp: n.AssignedIP, Status: n.Status, NetworkRevision: revision})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].NodeId < targets[j].NodeId })
	return targets, nil
}

func (r *Revocations) execute(ctx context.Context, p state.Profile, command state.RemoteRevocation, query bool) (contract.NodeRevocationReceipt, error) {
	stored, ok := p.RemoteRevocations[command.Request.CommandID]
	if !ok || stored.Request != command.Request || command.Request.Validate() != nil || p.Attempt == nil || command.UserID != p.Secrets.UserID || command.ControlOrigin != p.ControlOrigin || command.Request.AccountID != p.Attempt.AccountID || command.Request.NetworkID != p.Attempt.NetworkID {
		return contract.NodeRevocationReceipt{}, ErrTargetProtocol
	}
	path := "/networks/" + url.PathEscape(command.Request.NetworkID) + "/nodes/" + url.PathEscape(command.Request.NodeID) + "/revocations"
	method := http.MethodPost
	var body []byte
	if query {
		method = http.MethodGet
		path += "/" + url.PathEscape(command.Request.CommandID)
	} else {
		var err error
		body, err = json.Marshal(command.Request)
		if err != nil {
			return contract.NodeRevocationReceipt{}, ErrTargetProtocol
		}
	}
	raw, _, err := r.call(ctx, p, method, path, body)
	if err != nil {
		return contract.NodeRevocationReceipt{}, err
	}
	result, err := contract.DecodeNodeRevocationReceipt(raw, r.config.MaxBytes)
	if err != nil || result.Match(command.Request) != nil || r.config.Now().Before(result.AcceptedAt) {
		return contract.NodeRevocationReceipt{}, ErrTargetProtocol
	}
	return result, nil
}

func (r *Revocations) Revoke(ctx context.Context, p state.Profile, c state.RemoteRevocation) (contract.NodeRevocationReceipt, error) {
	return r.execute(ctx, p, c, false)
}
func (r *Revocations) Get(ctx context.Context, p state.Profile, c state.RemoteRevocation) (contract.NodeRevocationReceipt, error) {
	return r.execute(ctx, p, c, true)
}
