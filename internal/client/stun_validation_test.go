package client

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/stunclient"
)

func TestSTUNResponseValidatesEntireMessage(t *testing.T) {
	req, tx, err := stunclient.BuildBindingRequest()
	if err != nil {
		t.Fatal(err)
	}
	response, err := buildSTUNBindingResponseForTest(req, &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		attr []byte
		good bool
	}{
		{"optional", []byte{0x80, 0x22, 0, 0}, true},
		{"required", []byte{0, 0x0f, 0, 0}, false},
		{"truncated", []byte{0x80, 0x22, 0, 8}, false},
		{"duplicate", response[20:], true},
		{"invalid_duplicate", []byte{0, 0x20, 0, 0}, false},
	} {
		for _, before := range []bool{true, false} {
			t.Run(tc.name+map[bool]string{true: "_before", false: "_after"}[before], func(t *testing.T) {
				p := append([]byte(nil), response[:20]...)
				if before {
					p = append(p, tc.attr...)
					p = append(p, response[20:]...)
				} else {
					p = append(p, response[20:]...)
					p = append(p, tc.attr...)
				}
				binary.BigEndian.PutUint16(p[2:4], uint16(len(p)-20))
				mapped, err := stunclient.ParseBindingResponse(p, tx)
				if (err == nil) != tc.good {
					t.Fatalf("accepted=%v err=%v", err == nil, err)
				}
				if tc.good && mapped.Port != 1234 {
					t.Fatal("wrong mapping")
				}
			})
		}
	}
}

func TestMagicBindIgnoresInvalidAndForeignSTUNReplies(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "recovery"}[valid], func(t *testing.T) {
			bind := NewMagicBind()
			_, _, err := bind.Open(0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = bind.Close() }()
			server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = server.Close() }()
			done := make(chan error, 1)
			go func() {
				buf := make([]byte, 2048)
				n, remote, err := server.ReadFromUDP(buf)
				if err != nil {
					done <- err
					return
				}
				response, err := buildSTUNBindingResponseForTest(buf[:n], remote)
				if err != nil {
					done <- err
					return
				}
				foreign := *server.LocalAddr().(*net.UDPAddr)
				foreign.Port++
				bind.dispatchSTUN(response, &foreign)
				bad := append(append([]byte(nil), response...), 0x80, 0x22, 0, 8)
				binary.BigEndian.PutUint16(bad[2:4], uint16(len(bad)-20))
				bind.dispatchSTUN(bad, server.LocalAddr().(*net.UDPAddr))
				if valid {
					_, err = server.WriteToUDP(response, remote)
				}
				done <- err
			}()
			mapped, err := bind.querySTUN(context.Background(), server.LocalAddr().String(), 200*time.Millisecond)
			if valid {
				if err != nil || mapped.Port != bind.LocalPort() {
					t.Fatalf("valid reply not accepted: %v %v", mapped, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "timed out") {
				t.Fatalf("expected timeout, got %v", err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
