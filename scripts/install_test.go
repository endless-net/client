//go:build linux

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func shellFixture(t *testing.T, script string, tools map[string]string, input string, settings ...string) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range tools {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "state"), []byte("installed 0.3.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", script)
	cmd.Env = append(cmd.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "TMPDIR="+root, "FIXTURE="+root, "COMMAND_LOG="+filepath.Join(root, "commands"))
	cmd.Env = append(cmd.Env, settings...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	log, readErr := os.ReadFile(filepath.Join(root, "commands"))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	return string(out), string(log), err
}

func TestAptBootstrap(t *testing.T) {
	root := t.TempDir()
	osRelease := filepath.Join(root, "os-release")
	if err := os.WriteFile(osRelease, []byte("ID=ubuntu\nVERSION_ID=26.04\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "install.sh")
	const probe = ". /etc/os-release"
	if !strings.Contains(string(source), probe) {
		t.Fatal("installation script platform probe changed")
	}
	contents := strings.Replace(string(source), probe, `. "$FIXTURE_OS_RELEASE"`, 1)
	if err := os.WriteFile(script, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := map[string]string{
		"id": "echo 0", "dpkg": "echo amd64",
		"install": "printf 'install %s\\n' \"$*\" >> \"$COMMAND_LOG\"",
		"apt-get": "printf 'apt %s\\n' \"$*\" >> \"$COMMAND_LOG\"; [ \"$FAILURE\" != apt ]",
		"apt-cache": `case "$*" in
  'policy endlessnet-client') echo '  Candidate: 0.17.4' ;;
  'show endlessnet-client=0.17.4') [ "$FAILURE" = legacy ] || echo 'Provides: endlessnet-client-linux-runtime' ;;
esac`,
		"curl": `[ "$FAILURE" != curl ] || exit 1
for arg do output="$arg"; done
[ "$FAILURE" = empty ] || printf fixture-key > "$output"`,
	}
	for _, failure := range []string{"", "curl", "empty", "apt", "legacy"} {
		name := failure
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			output, commands, err := shellFixture(t, script, tools, "", "FAILURE="+failure, "FIXTURE_OS_RELEASE="+osRelease)
			if (err == nil) != (failure == "") {
				t.Fatalf("unexpected result: %v\n%s", err, output)
			}
			switch failure {
			case "":
				for _, want := range []string{"/etc/apt/keyrings/endlessnet.gpg", "apt -o APT::Update::Error-Mode=any update", "apt install -y endlessnet-client=0.17.4"} {
					if !strings.Contains(commands, want) {
						t.Fatalf("missing %s in %s", want, commands)
					}
				}
			case "curl", "empty":
				if commands != "" {
					t.Fatalf("configured APT without a key: %s", commands)
				}
			case "apt":
				if strings.Contains(commands, "apt install") || strings.Contains(output, "Client установлен") {
					t.Fatal("reported install after failed index")
				}
			case "legacy":
				if strings.Contains(commands, "apt install") || strings.Contains(output, "Client установлен") {
					t.Fatal("accepted the obsolete APT package")
				}
			}
		})
	}
}

func TestCheckUpdates(t *testing.T) {
	tools := map[string]string{
		"id": "echo 0", "dpkg": "exec /usr/bin/dpkg \"$@\"",
		"dpkg-query": "if [ \"$FAILURE\" = status ]; then echo 'half-configured 0.3.0'; else cat \"$FIXTURE/state\"; fi",
		"apt-cache":  "printf '  Candidate: %s\\n' \"$CANDIDATE\"; [ \"$FAILURE\" != cache ]",
		"apt-get": `printf '%s\n' "$*" >> "$COMMAND_LOG"
if [ "$1" = install ]; then
  [ "$FAILURE" != install ] || exit 1
  [ "$FAILURE" = nochange ] || printf 'installed %s' "$CANDIDATE" > "$FIXTURE/state"
else
  [ "$FAILURE" != index ] || exit 1
fi`,
	}
	for _, tc := range []struct {
		name, candidate, answer, failure, want string
		installs, success                      bool
	}{
		{"equal", "0.3.0", "n\n", "", "Обновлений нет", false, true},
		{"older", "0.2.0", "n\n", "", "Обновлений нет", false, true},
		{"no", "0.4.0", "n\n", "", "Обновление отменено", false, true},
		{"EOF", "0.4.0", "", "", "Обновление отменено", false, true},
		{"blank", "0.4.0", "\n", "", "Обновление отменено", false, true},
		{"confirmed", "0.4.0", "y\n", "", "Client обновлён до 0.4.0", true, true},
		{"index", "0.4.0", "y\n", "index", "", false, false},
		{"install", "0.4.0", "y\n", "install", "", true, false},
		{"nochange", "0.4.0", "y\n", "nochange", "", true, false},
		{"missing", "(none)", "y\n", "", "", false, false},
		{"status", "0.4.0", "y\n", "status", "", false, false},
		{"cache", "0.4.0", "y\n", "cache", "Не удалось прочитать", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launcher := filepath.Join(t.TempDir(), "run")
			if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec sh ./endlessnet check-updates\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			output, commands, err := shellFixture(t, launcher, tools, tc.answer, "FAILURE="+tc.failure, "CANDIDATE="+tc.candidate)
			if (err == nil) != tc.success || !strings.Contains(output, tc.want) ||
				strings.Contains(commands, "install --only-upgrade -y endlessnet-client=0.4.0") != tc.installs ||
				(!tc.success && (strings.Contains(output, "Client обновлён") || strings.Contains(output, "Обновлений нет"))) ||
				(tc.failure == "status" && commands != "") ||
				(tc.failure == "cache" && strings.Contains(output, "Установить обновление")) {
				t.Fatalf("unexpected update: %v\n%s\n%s", err, output, commands)
			}
		})
	}
}

// Ensure the Bash fetch entrypoint keeps credentials scoped even on failure.
func TestPrivateAPIFetch(t *testing.T) {
	script, err := filepath.Abs("fetch-private-api.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, token, event, status string
		success                    bool
	}{
		{"missing token", "", "push", "97", false},
		{"untrusted event", "fixture-token", "pull_request_target", "97", false},
		{"successful download", "fixture-token", "push", "0", true},
		{"failed download", "fixture-token", "push", "42", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			where := t.TempDir()
			fake := filepath.Join(where, "go")
			program := `#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'mod download all' ]]
[[ "$GOWORK" == off && "$GIT_TERMINAL_PROMPT" == 0 ]]
[[ "$GOPRIVATE" == 'github.com/endless-net/*' ]]
[[ "$GONOPROXY" == "$GOPRIVATE" && "$GONOSUMDB" == "$GOPRIVATE" ]]
[[ "$GIT_CONFIG_KEY_0" == credential.helper && -z "$GIT_CONFIG_VALUE_0" ]]
[[ "$GIT_CONFIG_KEY_1" == core.askPass && "$GIT_CONFIG_VALUE_1" == "$GIT_ASKPASS" ]]
[[ "$GIT_CONFIG_KEY_2" == credential.useHttpPath && "$GIT_CONFIG_VALUE_2" == false ]]
[[ "$(stat -c %a "$GIT_ASKPASS")" == 700 ]]
[[ "$("$GIT_ASKPASS" "Username for 'https://github.com': ")" == x-access-token ]]
[[ "$("$GIT_ASKPASS" "Password for 'https://x-access-token@github.com': ")" == "$ENDLESSNET_API_READ_TOKEN" ]]
if "$GIT_ASKPASS" "Password for 'https://other.example': "; then exit 98; fi
exit "$FIXTURE_DOWNLOAD_EXIT"
`
			if err := os.WriteFile(fake, []byte(program), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "bash", script)
			cmd.Dir = where
			cmd.Env = []string{
				"PATH=" + where + ":/usr/bin:/bin", "TMPDIR=" + where, "GOWORK=ambient-workspace",
				"ENDLESSNET_API_READ_TOKEN=" + tc.token, "GITHUB_EVENT_NAME=" + tc.event,
				"FIXTURE_DOWNLOAD_EXIT=" + tc.status,
			}
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.success || strings.Contains(string(out), "fixture-token") {
				t.Fatalf("unexpected exit: %v\n%s", err, out)
			}
			if !tc.success {
				exit, ok := err.(*exec.ExitError)
				code := 1
				if tc.status == "42" {
					code = 42
				}
				if !ok || exit.ExitCode() != code {
					t.Fatalf("expected exit %d, got %v", code, err)
				}
			}
			entries, err := os.ReadDir(where)
			if err != nil || len(entries) != 1 || entries[0].Name() != "go" {
				t.Fatalf("unexpected checkout after fetch: %v %v", entries, err)
			}
		})
	}
}
