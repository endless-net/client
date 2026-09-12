package client

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestDNSUpstreamRejectsUnrelatedResponseAndRecovers(t *testing.T) {
	for _, fault := range []string{"id", "name", "type", "class", "query", "opcode", "missing-question"} {
		t.Run(fault, func(t *testing.T) {
			server, err := net.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = server.Close() }()
			go func() {
				for i := range 2 {
					var buffer [512]byte
					n, remote, err := server.ReadFrom(buffer[:])
					if err != nil {
						return
					}
					var reply dnsmessage.Message
					if reply.Unpack(buffer[:n]) != nil {
						return
					}
					reply.Response = true
					if i == 0 {
						switch fault {
						case "id":
							reply.ID++
						case "name":
							reply.Questions[0].Name, _ = dnsmessage.NewName("other.example.")
						case "type":
							reply.Questions[0].Type = dnsmessage.TypeAAAA
						case "class":
							reply.Questions[0].Class = dnsmessage.ClassCHAOS
						case "query":
							reply.Response = false
						case "opcode":
							reply.OpCode = 1
						case "missing-question":
							reply.Questions = nil
						}
					}
					wire, err := reply.Pack()
					if err != nil {
						return
					}
					_, _ = server.WriteTo(wire, remote)
				}
			}()
			request := dnsTestQuery(t, 0x1234, "public.example", dnsTypeA)
			if _, err := forwardDNSQuery(context.Background(), server.LocalAddr().String(), request, time.Second); err == nil {
				t.Error("unrelated upstream response was accepted")
			}
			if _, err := forwardDNSQuery(context.Background(), server.LocalAddr().String(), request, time.Second); err != nil {
				t.Fatal("valid upstream response did not recover")
			}
		})
	}
}
