package tests

import (
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// HC-025/HC-026: real CLI, DNS wire faults and recovery in one proxy process.
func TestControlPlaneDNSUpstreamResponseBinding(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		for _, mode := range []string{"udp-answer", "tcp-answer", "invalid-truncated-udp"} {
			fallback := mode != "udp-answer"
			t.Run(network+"/"+mode, func(t *testing.T) {
				_, n, _ := controlScenario(t)
				n.Stop()
				var fault atomic.Int32
				var udpQueries, tcpQueries atomic.Int32
				upstream, _, setCode := dnsContractUpstream(t, network, fallback, [4]byte{203, 0, 113, 4}, func(reply *dnsmessage.Message, tcp bool) {
					if tcp {
						tcpQueries.Add(1)
					} else {
						udpQueries.Add(1)
					}
					if mode == "tcp-answer" && !tcp || mode == "invalid-truncated-udp" && tcp {
						return
					}
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
						beforeUDP, beforeTCP := udpQueries.Load(), tcpQueries.Load()
						assertDNSWire(t, transport, address, "public.example.", dnsmessage.RCodeServerFailure, "")
						wantTCP := int32(0)
						if mode == "tcp-answer" {
							wantTCP = 1
						}
						if udpQueries.Load()-beforeUDP != 1 || tcpQueries.Load()-beforeTCP != wantTCP {
							t.Fatal("invalid upstream answer used an unexpected transport path")
						}
						fault.Store(0)
						beforeUDP, beforeTCP = udpQueries.Load(), tcpQueries.Load()
						assertDNSWire(t, transport, address, "public.example.", dnsmessage.RCodeSuccess, "203.0.113.4")
						if fallback {
							wantTCP = 1
						}
						if udpQueries.Load()-beforeUDP != 1 || tcpQueries.Load()-beforeTCP != wantTCP {
							t.Fatal("repaired upstream answer did not use the required transport path")
						}
					}
					// A correlated negative answer is a valid DNS outcome. Preserve
					// its RCODE instead of turning every upstream rejection into SERVFAIL.
					for _, code := range []dnsmessage.RCode{dnsmessage.RCodeNameError, dnsmessage.RCodeRefused} {
						setCode(code)
						beforeUDP, beforeTCP := udpQueries.Load(), tcpQueries.Load()
						assertDNSWire(t, transport, address, "public.example.", code, "")
						wantTCP := int32(0)
						if fallback {
							wantTCP = 1
						}
						if udpQueries.Load()-beforeUDP != 1 || tcpQueries.Load()-beforeTCP != wantTCP {
							t.Fatal("valid negative DNS answer did not use the required transport path")
						}
						setCode(dnsmessage.RCodeSuccess)
						assertDNSWire(t, transport, address, "public.example.", dnsmessage.RCodeSuccess, "203.0.113.4")
					}
				}
			})
		}
	}
}
