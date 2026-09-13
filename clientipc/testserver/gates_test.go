package testserver

import (
	"strings"
	"testing"
)

func TestScriptResponseGates(t *testing.T) {
	for _, names := range []string{
		`["one"]`, `["same","same"]`, `["","bad name"]`, `["","UPPER"]`,
		`["","` + strings.Repeat("a", 65) + `"]`,
	} {
		_, err := Load(strings.NewReader(`{"steps":[{"method":"WatchEvents","request":{},"responses":[{},{}],"response_gates":` + names + `}]}`))
		if err == nil {
			t.Fatal("invalid gate list accepted")
		}
	}
	for _, invalid := range []string{"unknown-private-marker", "next"} {
		server, err := Load(strings.NewReader(`{"steps":[{"method":"WatchEvents","request":{},"responses":[{},{}],"response_gates":["","next"]}]}`))
		if err != nil {
			t.Fatal(err)
		}
		gate := server.steps["WatchEvents"][0].ResponseRelease[1]
		select {
		case <-gate:
			t.Fatal("gate was initially released")
		default:
		}
		if err := server.ReleaseGate("next"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-gate:
		default:
			t.Fatal("gate did not release")
		}
		if err := server.ReleaseGate(invalid); err == nil || err.Error() != "invalid or already released response gate" {
			t.Fatal("unknown/duplicate release did not fail safely")
		}
		if err := server.Verify(); err == nil || err.Error() != "invalid or already released response gate" {
			t.Fatal("release failure lost from verification")
		}
	}
}
