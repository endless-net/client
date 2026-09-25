package client

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestObserveLinuxDefaultRoutes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		outputs     []string
		failAt      int
		wantCalls   int
		wantPresent bool
		wantErr     bool
	}{
		{name: "none", outputs: []string{"[]", "[]"}, wantCalls: 2},
		{name: "ipv4", outputs: []string{`[{"dst":"default","dev":"eth0"}]`, "[]"}, wantCalls: 2, wantPresent: true},
		{name: "ipv6", outputs: []string{"[]", `[{"dst":"::/0","dev":"eth0"}]`}, wantCalls: 2, wantPresent: true},
		{name: "command failure", outputs: []string{"[]", "[]"}, failAt: 2, wantCalls: 2, wantErr: true},
		{name: "malformed result", outputs: []string{`[{"dst":"198.51.100.0/24"}]`, "[]"}, wantCalls: 1, wantErr: true},
		{name: "malformed json", outputs: []string{"not-json", "[]"}, wantCalls: 1, wantErr: true},
		{name: "unsupported family", outputs: []string{`[{"dst":"default","type":"nat"}]`, "[]"}, wantCalls: 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			present, err := observeDefaultRoutes(context.Background(), "linux", func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls++
				if name != "ip" || len(args) != 5 || args[0] != "-j" || args[2] != "route" || args[3] != "show" || args[4] != "default" {
					t.Fatal("default route observation used an unexpected command")
				}
				if tc.failAt == calls {
					return nil, errors.New("private command output")
				}
				return []byte(tc.outputs[calls-1]), nil
			})
			if calls != tc.wantCalls || present != tc.wantPresent || (err != nil) != tc.wantErr {
				t.Fatalf("unexpected default route observation: present=%t observed=%t calls=%d", present, err == nil, calls)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("raw command error escaped observation")
			}
		})
	}
}

func TestObserveDefaultRoutesIsBoundedAndUnsupportedIsUnknown(t *testing.T) {
	if _, err := observeDefaultRoutes(context.Background(), "linux", func(context.Context, string, ...string) ([]byte, error) {
		return make([]byte, defaultRouteOutputLimit+1), nil
	}); err == nil {
		t.Fatal("oversized route table was accepted")
	}
	if _, err := observeDefaultRoutes(context.Background(), "plan9", func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unsupported platform invoked command")
		return nil, nil
	}); err == nil {
		t.Fatal("unsupported platform claimed an observation")
	}
}

func TestObserveDarwinDefaultRoutes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		outputs     []string
		wantPresent bool
		wantErr     bool
	}{
		{name: "none", outputs: []string{"Routing tables\nInternet:\nDestination Gateway Flags Netif Expire\n", "Routing tables\nInternet6:\nDestination Gateway Flags Netif Expire\n"}},
		{name: "ipv4", outputs: []string{"Routing tables\nInternet:\nDestination Gateway Flags Netif Expire\ndefault 192.0.2.1 UGSc en0\n", "Routing tables\nInternet6:\nDestination Gateway Flags Netif Expire\n"}, wantPresent: true},
		{name: "invalid second family", outputs: []string{"Routing tables\nInternet:\nDestination Gateway Flags Netif Expire\n", "unrecognized"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			present, err := observeDefaultRoutes(context.Background(), "darwin", func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls++
				family := []string{"inet", "inet6"}[calls-1]
				if name != "netstat" || len(args) != 3 || args[0] != "-rn" || args[1] != "-f" || args[2] != family {
					t.Fatal("default route observation used an unexpected command")
				}
				return []byte(tc.outputs[calls-1]), nil
			})
			if calls != 2 || present != tc.wantPresent || (err != nil) != tc.wantErr {
				t.Fatalf("unexpected default route observation: present=%t observed=%t calls=%d", present, err == nil, calls)
			}
		})
	}
}
