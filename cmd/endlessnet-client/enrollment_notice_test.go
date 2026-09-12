package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestBrowserEnrollmentNoticeFollowsDurableSave(t *testing.T) {
	for _, failNotice := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "notice failure"}[failNotice], func(t *testing.T) {
			const approvalURL = "https://admin.example.test/approve/request-1"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/nodes/enrollment-requests" {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(clientapi.CreateNodeEnrollmentRequestResponse{
					Request:   clientapi.NodeEnrollmentRequest{ID: "request-1", Status: clientapi.NodeEnrollmentRequestPending, ApprovalURL: approvalURL},
					PollToken: "synthetic-poll-token",
				})
			}))
			defer server.Close()
			dir := t.TempDir()
			setInstallationStateDirForTest(t, filepath.Join(dir, "installation-state"))
			path := filepath.Join(dir, "client.json")
			cfg := client.Config{}
			req := clientapi.RegisterNodeRequest{Hostname: "node-a", IdempotencyID: "request-proof"}
			noticeFailure := errors.New("cannot record user action")
			calls := 0
			_, err := waitForBrowserEnrollmentApproval(t.Context(), clientapi.NewAPI(server.URL, ""), &cfg, path, &req, 0, func(notice enrollmentApprovalRequiredError) error {
				calls++
				if notice.RequestID != "request-1" || notice.ApprovalURL != approvalURL {
					t.Fatal("incorrect approval notice")
				}
				saved, err := client.LoadConfig(path)
				if err != nil {
					t.Fatal(err)
				}
				if saved.EnrollmentRequestID != notice.RequestID || saved.ApprovalURL != notice.ApprovalURL || saved.EnrollmentPollToken != "synthetic-poll-token" || saved.EnrollmentRequest == nil || saved.EnrollmentRequest.IdempotencyID != req.IdempotencyID {
					t.Fatal("notice preceded durable enrollment state")
				}
				if failNotice {
					return noticeFailure
				}
				return nil
			})
			var pending enrollmentApprovalRequiredError
			if failNotice && !errors.Is(err, noticeFailure) || !failNotice && !errors.As(err, &pending) {
				t.Fatalf("unexpected enrollment result: %v", err)
			}
			if calls != 1 {
				t.Fatalf("notice calls = %d, want 1", calls)
			}
			saved, err := client.LoadConfig(path)
			if err != nil || saved.EnrollmentRequestID != "request-1" || saved.EnrollmentRequest == nil {
				t.Fatal("pending enrollment lost after notice")
			}
		})
	}
}
