package control

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	contract "github.com/endless-net/coordinator/api/enrollment"
)

func TestRevocationTransportRequiresCurrentRevisionAndNeverReplaysMutation(t *testing.T) {
	for _, mode := range []string{"valid", "missing-revision", "duplicate-revision", "foreign-node", "lost-answer", "false-enforced", "expired-session"} {
		t.Run(mode, func(t *testing.T) {
			f := newRegistrationFixture(t)
			f.p.Attempt.Phase = enrollment.Registered
			f.p.Attempt.NodeID = "available"
			f.p.Secrets.UserID = "human"
			req := contract.RevokeNodeRequest{AccountID: "account", NetworkID: "network", NodeID: "lost", CommandID: "c086b75d-a2a6-4d4e-9bb8-6ce3bc0b674e", ExpectedRevision: 7}
			command := state.RemoteRevocation{CallerSID: "owner", UserID: "human", ControlOrigin: f.p.ControlOrigin, CreatedAt: f.now, Request: req}
			f.p.RemoteRevocations = map[string]state.RemoteRevocation{req.CommandID: command}
			calls := 0
			r, err := NewRevocations(&http.Client{Transport: transport(func(request *http.Request) (*http.Response, error) {
				calls++
				if _, ok := request.Context().Deadline(); !ok || request.Header.Get("Authorization") != "Bearer "+f.p.Secrets.Access || request.Header.Get("Cookie") != "" || request.Header.Get("X-EndlessNet-Node-Credential") != "" || request.GetBody != nil {
					t.Fatal("wrong credentials or replayable request")
				}
				header := http.Header{"Content-Type": {"application/json"}}
				header.Set(contract.NodeListRevisionHeader, "7")
				var raw []byte
				if request.Method == http.MethodGet {
					network := "network"
					if mode == "foreign-node" {
						network = "foreign"
					}
					raw = encode(t, []contract.Node{{ID: "lost", NetworkID: network, Hostname: "same-name", Status: "active"}})
					if mode == "missing-revision" {
						header.Del(contract.NodeListRevisionHeader)
					}
					if mode == "duplicate-revision" {
						header.Add(contract.NodeListRevisionHeader, "8")
					}
				} else {
					body, _ := io.ReadAll(request.Body)
					decoded, err := contract.DecodeRevokeNodeRequest(body, 65536)
					if err != nil || decoded != req || bytes.Contains(body, []byte(f.p.Secrets.Access)) {
						t.Fatal("command changed or exported a secret", err)
					}
					if mode == "lost-answer" {
						return nil, errors.New("private transport diagnostic")
					}
					receipt := contract.NodeRevocationReceipt{AccountID: "account", NetworkID: "network", NodeID: "lost", CommandID: req.CommandID, Status: "accepted", Revision: 8, AcceptedAt: f.now, EnforceBy: f.now.Add(300 * time.Second)}
					if mode == "false-enforced" {
						receipt.Status = "enforced"
					}
					raw = encode(t, receipt)
				}
				return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(bytes.NewReader(raw)), Request: request}, nil
			})}, staticAnnouncement{f.announcement}, RevocationConfig{MaxBytes: 65536, MaxTargets: 10, Now: func() time.Time { return f.now }})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "expired-session" {
				f.p.Secrets.SessionExpiresAt = f.now
			}
			targets, err := r.List(t.Context(), f.p)
			if mode == "missing-revision" || mode == "duplicate-revision" || mode == "foreign-node" || mode == "expired-session" {
				if err == nil || targets != nil {
					t.Fatal("unverified target/current revision accepted")
				}
				return
			}
			if err != nil || len(targets) != 1 || targets[0].NetworkRevision != 7 {
				t.Fatal("current target missing", err)
			}
			before := calls
			result, err := r.Revoke(t.Context(), f.p, command)
			if calls != before+1 {
				t.Fatal("hidden mutation replay")
			}
			if mode == "lost-answer" || mode == "false-enforced" {
				if err == nil || result.CommandID != "" {
					t.Fatal("unknown/false enforcement became success")
				}
			} else if err != nil || result.Match(req) != nil {
				t.Fatal("original accepted receipt rejected", err)
			}
		})
	}
}
