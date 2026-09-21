package client

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWireGuardHandshakeEvidenceKeepsSubsecondOrdering(t *testing.T) {
	key := strings.Repeat("01", 32)
	var got WireGuardInspection
	raw := "public_key=" + key + "\nlast_handshake_time_sec=100\nlast_handshake_time_nsec=700000000\n"
	if err := parseWireGuardEngineIPC(&got, raw); err != nil {
		t.Fatal(err)
	}
	handshake, ok := got.Peers[0].authenticatedHandshakeTime()
	if !ok || !handshake.Equal(time.Unix(100, 700000000)) || !handshake.After(time.Unix(100, 600000000)) || handshake.After(time.Unix(100, 800000000)) {
		t.Fatal("full timestamp cannot order handshake against path transition")
	}
	encoded, err := json.Marshal(got.Peers[0])
	if err != nil || strings.Contains(string(encoded), "700000000") || strings.Contains(string(encoded), "handshakeTimeComplete") {
		t.Fatal("private evidence changed public diagnostic representation", err)
	}
	var diagnostic WireGuardPeerInspection
	if err := json.Unmarshal(encoded, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if _, ok := diagnostic.authenticatedHandshakeTime(); ok {
		t.Fatal("public diagnostic fabricated full native evidence")
	}
}

func TestWireGuardHandshakeEvidenceRejectsIncompleteOrInvalidTime(t *testing.T) {
	key := "public_key=" + strings.Repeat("01", 32) + "\n"
	for _, fields := range []string{"", "last_handshake_time_sec=100\n", "last_handshake_time_nsec=1\n", "last_handshake_time_sec=0\nlast_handshake_time_nsec=0\n"} {
		var got WireGuardInspection
		if err := parseWireGuardEngineIPC(&got, key+fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := got.Peers[0].authenticatedHandshakeTime(); ok {
			t.Fatal("incomplete/zero handshake accepted")
		}
	}
	for _, fields := range []string{
		"last_handshake_time_sec=-1\n", "last_handshake_time_sec=invalid\n", "last_handshake_time_nsec=-1\n", "last_handshake_time_nsec=1000000000\n", "last_handshake_time_nsec=invalid\n",
		"last_handshake_time_sec=100\nlast_handshake_time_sec=100\n", "last_handshake_time_nsec=1\nlast_handshake_time_nsec=1\n",
	} {
		var got WireGuardInspection
		if parseWireGuardEngineIPC(&got, key+fields) == nil {
			t.Fatal("ambiguous timestamp accepted", fields)
		}
	}
	var got WireGuardInspection
	if err := parseWireGuardEngineIPC(&got, key+"last_handshake_time_nsec=5\nlast_handshake_time_sec=100\n"+key+"last_handshake_time_sec=101\n"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Peers[0].authenticatedHandshakeTime(); !ok {
		t.Fatal("reversed field order lost evidence")
	}
	if _, ok := got.Peers[1].authenticatedHandshakeTime(); ok {
		t.Fatal("timestamp fields leaked across peers")
	}
}
