// Command verify-source-ci gates publication on the exact source commit's CI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"
)

type workflowRun struct {
	ID         int64  `json:"id"`
	Attempt    int    `json:"run_attempt"`
	SHA        string `json:"head_sha"`
	Branch     string `json:"head_branch"`
	Event      string `json:"event"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type githubAPI struct {
	client                  *http.Client
	base, repository, token string
}

func (g githubAPI) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+"/repos/"+g.repository+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("GitHub CI evidence request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub CI evidence returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return errors.New("invalid GitHub CI evidence")
	}
	return nil
}

var requiredJobs = func() []string {
	jobs := []string{
		"verify", "Client control-plane scenarios", "Verify (Linux)", "Verify (Windows)", "Verify (macOS)",
		"Install and smoke (ubuntu-24.04)", "Install and smoke (ubuntu-22.04)",
		"Install and smoke (ubuntu-24.04-arm)", "Install and smoke (ubuntu-22.04-arm)",
		"Install and smoke (windows-2022)", "Install and smoke (windows-2025)",
		"Install and smoke (macos-15)", "Install and smoke (macos-15-intel)",
	}
	for _, platform := range []string{"ubuntu-22.04", "ubuntu-24.04", "ubuntu-22.04-arm", "ubuntu-24.04-arm", "windows-2022", "windows-2025", "macos-15", "macos-15-intel"} {
		for repetition := 1; repetition <= 3; repetition++ {
			jobs = append(jobs, fmt.Sprintf("Client contracts (%s, repeat %d)", platform, repetition))
		}
	}
	return jobs
}()

func (g githubAPI) verify(ctx context.Context, sha string) (workflowRun, error) {
	var list struct {
		Runs []workflowRun `json:"workflow_runs"`
	}
	// Select the newest matching run, including pending/failed runs. An older
	// success must never conceal a newer failed or still-running verification.
	query := url.Values{"head_sha": {sha}, "branch": {"main"}, "event": {"push"}, "per_page": {"100"}}
	if err := g.get(ctx, "/actions/workflows/test.yml/runs?"+query.Encode(), &list); err != nil {
		return workflowRun{}, err
	}
	var selected workflowRun
	for _, run := range list.Runs {
		if run.ID > selected.ID {
			selected = run
		}
	}
	valid := func(r workflowRun) bool {
		return r.ID > 0 && r.Attempt > 0 && r.SHA == sha && r.Branch == "main" && r.Event == "push" && r.Path == ".github/workflows/test.yml" && r.Status == "completed" && r.Conclusion == "success"
	}
	if !valid(selected) {
		return workflowRun{}, errors.New("exact source commit has no completed successful main push Test run")
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		var jobs struct {
			Jobs []struct{ Name, Status, Conclusion string } `json:"jobs"`
		}
		path := fmt.Sprintf("/actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", selected.ID, selected.Attempt, page)
		if err := g.get(ctx, path, &jobs); err != nil {
			return workflowRun{}, err
		}
		for _, job := range jobs.Jobs {
			for _, name := range requiredJobs {
				if job.Name != name {
					continue
				}
				if seen[name] || job.Status != "completed" || job.Conclusion != "success" {
					return workflowRun{}, fmt.Errorf("required CI job %q is not uniquely successful", name)
				}
				seen[name] = true
			}
		}
		if len(jobs.Jobs) < 100 {
			break
		}
	}
	for _, name := range requiredJobs {
		if !seen[name] {
			return workflowRun{}, fmt.Errorf("required CI job %q is missing", name)
		}
	}
	var current workflowRun
	if err := g.get(ctx, fmt.Sprintf("/actions/runs/%d", selected.ID), &current); err != nil {
		return workflowRun{}, err
	}
	if !valid(current) || current.ID != selected.ID || current.Attempt != selected.Attempt {
		return workflowRun{}, errors.New("CI evidence changed while checking publication gate")
	}
	return selected, nil
}

func main() {
	sha, repository, token := os.Getenv("SOURCE_SHA"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GH_TOKEN")
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(sha) || !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repository) || token == "" {
		fmt.Fprintln(os.Stderr, "SOURCE_SHA, GITHUB_REPOSITORY and GH_TOKEN are required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	g := githubAPI{client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: "https://api.github.com", repository: repository, token: token}
	run, err := g.verify(ctx, sha)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Verified source %s against Test run %d attempt %d\n", sha, run.ID, run.Attempt)
}
