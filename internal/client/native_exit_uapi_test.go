package client

import (
	"errors"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func nativeExitUAPIFixture(t *testing.T, family api.ExitFamilyMode) (string, *ClientExitSelection) {
	t.Helper()
	hexKey := strings.Repeat("12", 32)
	key, err := wireGuardHexToKey(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	selection := &ClientExitSelection{Host: api.ServiceHost{PublicKey: key}, Family: family}
	raw := "private_key=" + strings.Repeat("34", 32) + "\nlisten_port=51820\nfwmark=51999\nreplace_peers=true\npublic_key=" + hexKey + "\nendpoint=127.0.0.1:25000\nreplace_allowed_ips=true\nallowed_ip=100.64.0.2/32\n"
	if family != api.ExitFamilyIPv6Only {
		raw += "allowed_ip=0.0.0.0/0\n"
	}
	if family != api.ExitFamilyIPv4Only {
		raw += "allowed_ip=::/0\n"
	}
	raw += "public_key=" + strings.Repeat("56", 32) + "\nendpoint=192.0.2.3:51820\nallowed_ip=100.64.0.3/32\n"
	return raw, selection
}

func TestNativeExitUAPIProvesAllFamiliesAndCommittedRelayEndpoint(t *testing.T) {
	for _, family := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		expected, selection := nativeExitUAPIFixture(t, family)
		actual := strings.ReplaceAll(strings.ReplaceAll(expected, "replace_peers=true\n", ""), "replace_allowed_ips=true\n", "") + "rx_bytes=100\ntx_bytes=200\nlast_handshake_time_sec=1234\nerrno=0\n"
		if err := nativeExitUAPITextObserved(expected, actual, selection, 51999); err != nil {
			t.Fatal(family, err)
		}
		actual = strings.Replace(actual, "127.0.0.1:25000", "192.0.2.4:51820", 1)
		if nativeExitUAPITextObserved(expected, actual, selection, 51999) == nil {
			t.Fatal("endpoint outside committed relay path accepted")
		}
	}
}

func TestNativeExitUAPIRejectsLivePeerRouteMarkAndEndpointTampering(t *testing.T) {
	expected, selection := nativeExitUAPIFixture(t, api.ExitFamilyDualStack)
	for _, actual := range []string{
		strings.Replace(expected, "fwmark=51999", "fwmark=0", 1),
		strings.Replace(expected, "fwmark=51999\n", "", 1),
		strings.Replace(expected, "fwmark=51999", "fwmark=51999\nfwmark=51999", 1),
		strings.Replace(expected, "allowed_ip=0.0.0.0/0\n", "", 1),
		strings.Replace(expected, "allowed_ip=::/0\n", "", 1),
		strings.Replace(expected, "allowed_ip=0.0.0.0/0", "allowed_ip=0.0.0.0/1", 1),
		expected + "allowed_ip=0.0.0.0/1\n",
		expected + "allowed_ip=0.0.0.0/0\n",
		expected + "public_key=" + strings.Repeat("12", 32) + "\n",
		strings.Replace(expected, "endpoint=127.0.0.1:25000", "endpoint=192.0.2.200:51820", 1),
		strings.Replace(expected, "endpoint=127.0.0.1:25000", "endpoint=127.0.0.1:25000\nendpoint=127.0.0.1:25000", 1),
		strings.Replace(expected, "allowed_ip=100.64.0.2/32", "allowed_ip=100.64.0.2/24", 1),
		expected + "errno=5\n",
	} {
		err := nativeExitUAPITextObserved(expected, actual, selection, 51999)
		if !errors.Is(err, errNativeExitUAPI) || strings.Contains(err.Error(), "343434") {
			t.Fatal("tampering accepted or private state exposed")
		}
	}
}

func TestNativeExitUAPIRejectsInvalidCommittedOwnership(t *testing.T) {
	raw, selection := nativeExitUAPIFixture(t, api.ExitFamilyDualStack)
	for _, bad := range []string{
		raw + "allowed_ip=::/0\n",
		strings.Replace(raw, "allowed_ip=0.0.0.0/0\n", "", 1),
		strings.Replace(raw, "endpoint=127.0.0.1:25000\n", "", 1),
		raw + "allowed_ip=100.64.0.2/32\n",
	} {
		if nativeExitUAPITextObserved(bad, bad, selection, 51999) == nil {
			t.Fatal("invalid committed default ownership accepted")
		}
	}
	selection.Family = api.ExitFamilyIPv4Only
	if nativeExitUAPITextObserved(raw, raw, selection, 51999) == nil {
		t.Fatal("unrequested IPv6 default accepted")
	}
}

func TestNativeExitUAPIParsingIsBoundedAndMalformedStateFailsClosed(t *testing.T) {
	for _, raw := range []string{"", "not-an-assignment", strings.Repeat("x", 1<<20+1), "fwmark=51999\nendpoint=192.0.2.1:1\n", "fwmark=51999\npublic_key=bad\n", "fwmark=51999\nallowed_ip=0.0.0.0/0\n"} {
		if _, err := parseNativeExitUAPI(raw); err == nil {
			t.Fatal("invalid UAPI accepted")
		}
	}
	if nativeExitUAPIObserved(nil, nil) == nil || nativeExitUAPIObserved(&WireGuardEngine{}, nil) == nil {
		t.Fatal("missing device evidence accepted")
	}
}
