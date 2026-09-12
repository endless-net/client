package tests

import (
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// HC-025/HC-026: real CLI, DNS wire faults and recovery in one proxy process.
func TestControlPlaneDNSUpstreamResponseBinding(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		for _, fallback := range []bool{false, true} {
			mode := "udp-answer"
			if fallback {
				mode = "tcp-answer"
			}
			t.Run(network+"/"+mode, func(t *testing.T) {
				_, n, _ := controlScenario(t)
				n.Stop()
				var fault atomic.Int32
				upstream, _, _ := dnsContractUpstream(t, network, fallback, [4]byte{203, 0, 113, 4}, func(reply *dnsmessage.Message, tcp bool) {
					if fallback && !tcp {
						return
					} // Valid TC response must reach the faulty TCP answer.
					switch fault.Load() {
					case 1:
						reply.ID++
					case 2:
						reply.Questions[0].Name, _ = dnsmessage.NewName("other.example.")
					case 3:
						reply.Questions[0].Type = dnsmessage.TypeAAAA
					case 4:
						reply.Questions[0].Class = dnsmessage.ClassCHAOS
					case 5:
						reply.Response = false
					case 6:
						reply.OpCode = 1
					case 7:
						reply.Questions = nil
					}
				})
				host := "127.0.0.1"
				if network == "udp6" {
					host = "::1"
				}
				address, stop := startClientDNS(t, n, host, "--upstream", upstream)
				defer stop()
				for _, transport := range []string{"udp", "tcp"} {
					assertDNSWire(t, transport, address, "public.example.", dnsmessage.RCodeSuccess, "203.0.113.4")
					for i := int32(1); i <= 7; i++ {
						fault.Store(i)
						assertDNSWire(t, transport, address, "public.example.", dnsmessage.RCodeServerFailure, "")
						fault.Store(0)
						assertDNSWire(t, transport, address, "public.example.", dnsmessage.RCodeSuccess, "203.0.113.4")
					}
				}
			})
		}
	}
}
