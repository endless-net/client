//go:build windows || linux

package clientapp

import (
	"errors"
	"github.com/endless-net/client/internal/control"
	coord "github.com/endless-net/coordinator/api/enrollment"
	"net"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestLocalPostureRequiresCompleteInterfaceInventory(t *testing.T) {
	addr := func(ip string) net.Addr {
		return &net.IPNet{IP: net.ParseIP(ip).To4(), Mask: net.CIDRMask(24, 32)}
	}
	trusted := addr("10.0.0.9")
	other := addr("192.168.1.2")
	if got := postureLocalAddresses([]net.Addr{other, trusted, addr("127.0.0.1"), other}, nil); !slices.Equal(got, []string{"10.0.0.9", "192.168.1.2"}) {
		t.Fatal(got)
	}
	unreadable := &net.IPAddr{IP: net.ParseIP("192.168.1.2")}
	if got := postureLocalAddresses([]net.Addr{trusted, unreadable}, nil); got != nil {
		t.Fatal("partial address list accepted", got)
	}
	if got := postureLocalAddresses([]net.Addr{trusted}, errors.New("enumeration incomplete")); got != nil {
		t.Fatal("failed inventory accepted", got)
	}
	var many []net.Addr
	for i := 1; i <= 65; i++ {
		many = append(many, addr(net.IPv4(10, 0, 0, byte(i)).String()))
	}
	if got := postureLocalAddresses(many, nil); got != nil {
		t.Fatal("oversized inventory accepted", len(got))
	}
}

func TestNativePostureReachesEndpointWithoutExposingMeasurements(t *testing.T) {
	at := time.Now().UTC()
	m := collectNodePosture(at)
	if m == nil || !m.ObservedAt.Equal(at) || m.Platform != runtime.GOOS || m.KernelVersion == "" || coord.ValidateNodePostureMetadata(m) != nil {
		t.Fatal("native posture collection")
	}
	req := control.EndpointRequest("test-node", coord.UpdateNodeEndpointRequest{Generation: 1, TTL: "1m", Status: "online", Posture: m})
	if req.GetPosture().GetPlatform() != runtime.GOOS || !req.Posture.ObservedAt.AsTime().Equal(at) || req.Posture.HardwareSerial != m.HardwareSerial {
		t.Fatal("posture not bound to endpoint request")
	}
}
