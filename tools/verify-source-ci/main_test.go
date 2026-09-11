package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicationGate(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	good := workflowRun{ID: 42, Attempt: 2, SHA: sha, Branch: "main", Event: "push", Path: ".github/workflows/test.yml", Status: "completed", Conclusion: "success"}
	type job struct{ Name, Status, Conclusion string }
	type fixture struct {
		runs       []workflowRun
		current    workflowRun
		jobs       []job
		httpStatus int
		brokenJSON bool
	}
	for _, tc := range []struct {
		name   string
		mutate func(*fixture)
		wantOK bool
	}{
		{"verified", func(*fixture) {}, true},
		{"no-run", func(f *fixture) { f.runs = nil }, false},
		{"different-sha", func(f *fixture) { f.runs[0].SHA = strings.Repeat("b", 40) }, false},
		{"pull-request", func(f *fixture) { f.runs[0].Event = "pull_request" }, false},
		{"other-branch", func(f *fixture) { f.runs[0].Branch = "feature" }, false},
		{"other-workflow", func(f *fixture) { f.runs[0].Path = ".github/workflows/other.yml" }, false},
		{"newer-failure", func(f *fixture) { r := good; r.ID++; r.Conclusion = "failure"; f.runs = append(f.runs, r) }, false},
		{"newer-pending", func(f *fixture) {
			r := good
			r.ID++
			r.Status = "in_progress"
			r.Conclusion = ""
			f.runs = append(f.runs, r)
		}, false},
		{"missing-control-plane", func(f *fixture) { f.jobs = append(f.jobs[:1], f.jobs[2:]...) }, false},
		{"skipped-control-plane", func(f *fixture) { f.jobs[1].Conclusion = "skipped" }, false},
		{"failed-install", func(f *fixture) {
			for i := range f.jobs {
				if f.jobs[i].Name == "Install and smoke (windows-2025)" {
					f.jobs[i].Conclusion = "failure"
				}
			}
		}, false},
		{"missing-windows-contracts", func(f *fixture) {
			for i := range f.jobs {
				if f.jobs[i].Name == "Client contracts (windows-2025, repeat 3)" {
					f.jobs = append(f.jobs[:i], f.jobs[i+1:]...)
					break
				}
			}
		}, false},
		{"skipped-macos-contracts", func(f *fixture) {
			for i := range f.jobs {
				if f.jobs[i].Name == "Client contracts (macos-15, repeat 2)" {
					f.jobs[i].Conclusion = "skipped"
				}
			}
		}, false},
		{"missing-linux-arm-contracts", func(f *fixture) {
			for i := range f.jobs {
				if f.jobs[i].Name == "Client contracts (ubuntu-24.04-arm, repeat 1)" {
					f.jobs = append(f.jobs[:i], f.jobs[i+1:]...)
					break
				}
			}
		}, false},
		{"skipped-linux-arm-install", func(f *fixture) {
			for i := range f.jobs {
				if f.jobs[i].Name == "Install and smoke (ubuntu-22.04-arm)" {
					f.jobs[i].Conclusion = "skipped"
				}
			}
		}, false},
		{"duplicate-verify", func(f *fixture) { f.jobs = append(f.jobs, f.jobs[0]) }, false},
		{"attempt-changed", func(f *fixture) { f.current.Attempt++ }, false},
		{"run-restarted", func(f *fixture) { f.current.Status = "queued" }, false},
		{"api-denied", func(f *fixture) { f.httpStatus = 403 }, false},
		{"invalid-json", func(f *fixture) { f.brokenJSON = true }, false},
		{"paginated-jobs", func(f *fixture) { filler := make([]job, 100); f.jobs = append(filler, f.jobs...) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture{runs: []workflowRun{good}, current: good}
			for _, name := range requiredJobs {
				f.jobs = append(f.jobs, job{name, "completed", "success"})
			}
			tc.mutate(&f)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-only" {
					t.Error("missing authorization")
				}
				if f.httpStatus != 0 {
					w.WriteHeader(f.httpStatus)
					return
				}
				if f.brokenJSON {
					_, _ = w.Write([]byte(`{"truncated":`))
					return
				}
				var result any
				switch r.URL.Path {
				case "/repos/endless-net/client/actions/workflows/test.yml/runs":
					if r.URL.Query().Get("head_sha") != sha || r.URL.Query().Get("event") != "push" || r.URL.Query().Get("branch") != "main" {
						t.Error("source query is not scoped")
					}
					result = map[string]any{"workflow_runs": f.runs}
				case "/repos/endless-net/client/actions/runs/42/attempts/2/jobs":
					jobs := f.jobs
					if r.URL.Query().Get("page") == "2" {
						jobs = jobs[100:]
					} else if len(jobs) > 100 {
						jobs = jobs[:100]
					}
					result = map[string]any{"jobs": jobs}
				case "/repos/endless-net/client/actions/runs/42":
					result = f.current
				default:
					t.Errorf("unexpected API path %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(result)
			}))
			defer s.Close()
			g := githubAPI{client: s.Client(), base: s.URL, repository: "endless-net/client", token: "test-only"}
			run, err := g.verify(context.Background(), sha)
			if (err == nil) != tc.wantOK {
				t.Fatalf("gate success=%v, want %v (error: %v)", err == nil, tc.wantOK, err)
			}
			if tc.wantOK && run != good {
				t.Fatal("gate returned different evidence")
			}
		})
	}
}
