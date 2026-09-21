//go:build linux

package client

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

func exitLANWatchMessage(kind uint16, payload int) []byte {
	raw := make([]byte, unix.NLMSG_HDRLEN+payload)
	binary.NativeEndian.PutUint32(raw[:4], uint32(len(raw)))
	binary.NativeEndian.PutUint16(raw[4:6], kind)
	return raw
}

func TestExitLANRouteDatagramKernelChanges(t *testing.T) {
	kernel := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	var batch []byte
	for _, message := range []struct {
		kind uint16
		size int
	}{
		{unix.RTM_NEWLINK, unix.SizeofIfInfomsg}, {unix.RTM_DELLINK, unix.SizeofIfInfomsg},
		{unix.RTM_NEWADDR, unix.SizeofIfAddrmsg}, {unix.RTM_DELADDR, unix.SizeofIfAddrmsg},
		{unix.RTM_NEWROUTE, unix.SizeofRtMsg}, {unix.RTM_DELROUTE, unix.SizeofRtMsg},
		{unix.RTM_NEWRULE, unix.SizeofRtMsg}, {unix.RTM_DELRULE, unix.SizeofRtMsg},
	} {
		raw := exitLANWatchMessage(message.kind, message.size)
		if err := exitLANRouteDatagram(raw, 0, kernel); err != nil {
			t.Fatal("kernel change rejected", message.kind, err)
		}
		batch = append(batch, raw...)
	}
	if err := exitLANRouteDatagram(batch, 0, kernel); err != nil {
		t.Fatal("batched changes rejected", err)
	}
}

func TestExitLANRouteDatagramRejectsLossAndMalformedEvidence(t *testing.T) {
	for _, scenario := range []string{"empty", "oversize", "header_short", "size_short", "size_large", "payload_short", "tail", "header_pid", "sender_pid", "sender_family", "sender_type", "truncated", "control_truncated", "overrun", "error", "unknown", "attribute_short", "attribute_overrun", "batch_corrupt"} {
		t.Run(scenario, func(t *testing.T) {
			raw := exitLANWatchMessage(unix.RTM_NEWLINK, unix.SizeofIfInfomsg)
			var sender unix.Sockaddr = &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
			flags := 0
			switch scenario {
			case "empty":
				raw = nil
			case "oversize":
				raw = make([]byte, (64<<10)+1)
			case "header_short":
				raw = raw[:5]
			case "size_short":
				binary.NativeEndian.PutUint32(raw[:4], 15)
			case "size_large":
				binary.NativeEndian.PutUint32(raw[:4], uint32(len(raw)+4))
			case "payload_short":
				raw = exitLANWatchMessage(unix.RTM_NEWLINK, 1)
			case "tail":
				raw = append(raw, 1)
			case "header_pid":
				binary.NativeEndian.PutUint32(raw[12:16], 123)
			case "sender_pid":
				sender = &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Pid: 123}
			case "sender_family":
				sender = &unix.SockaddrNetlink{Family: unix.AF_INET}
			case "sender_type":
				sender = &unix.SockaddrInet4{}
			case "truncated":
				flags = unix.MSG_TRUNC
			case "control_truncated":
				flags = unix.MSG_CTRUNC
			case "overrun":
				raw = exitLANWatchMessage(unix.NLMSG_OVERRUN, 0)
			case "error":
				raw = exitLANWatchMessage(unix.NLMSG_ERROR, 4)
			case "unknown":
				raw = exitLANWatchMessage(65535, 0)
			case "attribute_short":
				raw = append(raw, 1, 0, 1, 0)
				binary.NativeEndian.PutUint32(raw[:4], uint32(len(raw)))
			case "attribute_overrun":
				raw = append(raw, 12, 0, 1, 0)
				binary.NativeEndian.PutUint32(raw[:4], uint32(len(raw)))
			case "batch_corrupt":
				raw = append(raw, exitLANWatchMessage(unix.NLMSG_OVERRUN, 0)...)
			}
			if err := exitLANRouteDatagram(raw, flags, sender); err == nil {
				t.Fatal("untrusted/lost event treated as well-formed")
			}
		})
	}
}
