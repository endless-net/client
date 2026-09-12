package tests

import (
	"errors"
	"testing"
	"time"
)

type sessionCommandWriter func([]byte) (int, error)

func (w sessionCommandWriter) Write(p []byte) (int, error) { return w(p) }

func TestSessionRecoveryRequiresExplicitFreshSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		responses  []string
		close      bool
		writeError bool
		attempts   int
	}{
		{name: "blocked-then-success", responses: []string{"blocked", "blocked", "ok"}, want: "recovered", attempts: 3},
		{name: "closed-process", close: true, want: "closed", attempts: 1},
		{name: "unexpected-readiness", responses: []string{"ready"}, want: "invalid-output", attempts: 1},
		{name: "unknown-output", responses: []string{"unexpected"}, want: "invalid-output", attempts: 1},
		{name: "command-failure", writeError: true, want: "command-failed"},
		{name: "no-response", want: "deadline", attempts: 1},
		{name: "denied-without-recovery", responses: []string{"blocked"}, want: "deadline", attempts: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := make(chan string, 1)
			sent := 0
			writer := sessionCommandWriter(func(p []byte) (int, error) {
				if string(p) != "exchange\n" {
					t.Fatal("observer changed the application session protocol")
				}
				if tc.writeError {
					return 0, errors.New("closed input")
				}
				if tc.close {
					close(lines)
				} else if sent < len(tc.responses) {
					lines <- tc.responses[sent]
				}
				sent++
				return len(p), nil
			})
			outcome, attempts := observeSessionRecovery(writer, lines, 100*time.Millisecond)
			if outcome != tc.want || attempts != tc.attempts {
				t.Fatalf("outcome=%s attempts=%d; want %s %d", outcome, attempts, tc.want, tc.attempts)
			}
		})
	}
}
