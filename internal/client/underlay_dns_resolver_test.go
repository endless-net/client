package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func underlayDNSTestSource() *underlayDNSSource {
	return &underlayDNSSource{Owner: ":1.42", Links: []underlayDNSLink{{Index: 2, Name: "eth0", Servers: []netip.AddrPort{netip.MustParseAddrPort("192.0.2.53:53")}, DefaultRoute: true, DNSSEC: "no", DNSOverTLS: "no"}}}
}

func TestUnderlayDNSDualFamilyPreservesUsableAnswer(t *testing.T) {
	for _, scenario := range []string{"ipv4_with_silent_ipv6", "ipv6_only", "delayed_ipv6", "caller_cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			goodSent := make(chan struct{})
			dial := func(context.Context, string, netip.AddrPort, underlayDNSLink) (net.Conn, error) {
				client, server := net.Pipe()
				go func() {
					defer func() { _ = server.Close() }()
					buf := make([]byte, 4096)
					n, err := server.Read(buf)
					if err != nil {
						return
					}
					reply := underlayDNSReply(t, buf[:n])
					kind := reply.Questions[0].Type
					if (scenario == "ipv4_with_silent_ipv6" || scenario == "caller_cancelled") && kind == dnsmessage.TypeAAAA {
						if scenario == "caller_cancelled" {
							<-goodSent
							cancel()
						}
						_, _ = server.Read(buf)
						return
					}
					if scenario == "ipv6_only" || scenario == "delayed_ipv6" {
						if kind == dnsmessage.TypeA {
							if scenario == "ipv6_only" {
								reply.Answers = nil
							}
						} else {
							if scenario == "delayed_ipv6" {
								timer := time.NewTimer(100 * time.Millisecond)
								select {
								case <-timer.C:
								case <-ctx.Done():
									timer.Stop()
									return
								}
							}
							reply.Answers[0].Header.Type = dnsmessage.TypeAAAA
							reply.Answers[0].Body = &dnsmessage.AAAAResource{AAAA: netip.MustParseAddr("2001:db8::80").As16()}
						}
					}
					raw, err := reply.Pack()
					if err != nil {
						t.Error(err)
						return
					}
					_, _ = server.Write(raw)
					if kind == dnsmessage.TypeA {
						close(goodSent)
					}
				}()
				return client, nil
			}
			r := newUnderlayDNSResolver(underlayDNSTestSource(), dial)
			addresses, err := r.lookup(ctx, "control.example", "tcp")
			if scenario == "caller_cancelled" {
				if !errors.Is(err, context.Canceled) || len(addresses) != 0 {
					t.Fatal("usable answer ignored caller cancellation", addresses, err)
				}
				return
			}
			if scenario == "delayed_ipv6" {
				if err != nil || len(addresses) != 2 || !slices.Contains(addresses, netip.MustParseAddr("192.0.2.80")) || !slices.Contains(addresses, netip.MustParseAddr("2001:db8::80")) {
					t.Fatal("slower usable family was discarded", addresses, err)
				}
				return
			}
			want := "192.0.2.80"
			if scenario == "ipv6_only" {
				want = "2001:db8::80"
			}
			if err != nil || len(addresses) != 1 || addresses[0].String() != want {
				t.Fatal("usable family lost", addresses, err)
			}
		})
	}
}

func TestUnderlayDNSCNAMEContinuationUsesAliasRoute(t *testing.T) {
	for _, scenario := range []string{"chain", "cycle", "selected_route_error", "depth"} {
		t.Run(scenario, func(t *testing.T) {
			source := underlayDNSTestSource()
			link := source.Links[0]
			link.Index = 3
			link.Name = "eth1"
			link.DefaultRoute = false
			link.Domains = []underlayDNSDomain{{Name: "private.example", RouteOnly: true}}
			link.Servers = []netip.AddrPort{netip.MustParseAddrPort("192.0.2.54:53")}
			source.Links = append(source.Links, link)
			var queried []string
			steps := 0
			dial := func(_ context.Context, _ string, serverIP netip.AddrPort, scope underlayDNSLink) (net.Conn, error) {
				client, server := net.Pipe()
				go func() {
					defer func() { _ = server.Close() }()
					buf := make([]byte, 4096)
					n, err := server.Read(buf)
					if err != nil {
						return
					}
					reply := underlayDNSReply(t, buf[:n])
					name := reply.Questions[0].Name.String()
					queried = append(queried, name)
					steps++
					if name == "control.example." {
						if scope.Index != 2 || serverIP.String() != "192.0.2.53:53" {
							t.Error("initial question escaped default source")
						}
					} else if scope.Index != 3 || serverIP.String() != "192.0.2.54:53" {
						t.Error("alias escaped its specific source route")
					}
					if scenario == "selected_route_error" && name != "control.example." {
						return
					}
					if name == "control.example." || scenario == "cycle" || scenario == "depth" {
						alias := "edge.private.example."
						if name != "control.example." && scenario == "cycle" {
							alias = "control.example."
						}
						if scenario == "depth" {
							alias = strings.Repeat("x", steps) + ".private.example."
						}
						target, _ := dnsmessage.NewName(alias)
						reply.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: reply.Questions[0].Name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET}, Body: &dnsmessage.CNAMEResource{CNAME: target}}}
					}
					raw, err := reply.Pack()
					if err != nil {
						t.Error(err)
						return
					}
					_, _ = server.Write(raw)
				}()
				return client, nil
			}
			r := newUnderlayDNSResolver(source, dial)
			addresses, err := r.lookup(t.Context(), "control.example", "tcp4")
			if scenario == "chain" {
				if err != nil || len(addresses) != 1 {
					t.Fatal("CNAME-only continuation failed", addresses, err)
				}
			} else if err == nil || len(addresses) != 0 {
				t.Fatal("invalid alias chain accepted", addresses, err)
			}
			want := 2
			if scenario == "depth" {
				want = 9
			}
			if len(queried) != want {
				t.Fatal("unexpected alias retries or fallback", queried)
			}
		})
	}
}

func TestUnderlayDNSRouteSelectionDoesNotFallThrough(t *testing.T) {
	source := underlayDNSTestSource()
	special := source.Links[0]
	special.Index = 3
	special.Name = "eth1"
	special.DefaultRoute = false
	special.Servers = nil
	special.Domains = []underlayDNSDomain{{Name: "private.example", RouteOnly: true}}
	source.Links = append(source.Links, special)
	global := source.Links[0]
	global.Index = 0
	global.Name = ""
	global.DefaultRoute = false
	source.Links = append(source.Links, global)
	r := newUnderlayDNSResolver(source, nil)
	for _, tc := range []struct {
		host    string
		indices []int
	}{
		{"control.example", []int{2, 0}},
		{"host.private.example", []int{3}},
		{"evilprivate.example", []int{2, 0}},
	} {
		links, err := r.links(tc.host)
		if err != nil {
			t.Fatal(err)
		}
		var indices []int
		for _, link := range links {
			indices = append(indices, link.Index)
		}
		if !reflect.DeepEqual(indices, tc.indices) {
			t.Fatal("wrong domain route", tc.host, indices)
		}
	}
	if _, err := r.links("host.local"); err == nil {
		t.Fatal("multicast name leaked to default DNS")
	}
	source.Links[1].Domains[0].Name = "changed.example"
	links, err := r.links("host.private.example")
	if err != nil || len(links) != 1 || links[0].Index != 3 {
		t.Fatal("source mutation changed immutable resolver")
	}
	if addresses, err := r.lookup(t.Context(), "host.private.example", "tcp4"); err == nil || len(addresses) != 0 {
		t.Fatal("empty specific route fell through to default DNS")
	}
}

func underlayDNSPipeDial(t *testing.T, answer func(string, []byte) []byte, calls *[]string) underlayDNSSocketDial {
	t.Helper()
	return func(ctx context.Context, network string, server netip.AddrPort, link underlayDNSLink) (net.Conn, error) {
		if server.String() != "192.0.2.53:53" || link.Index != 2 || link.Name != "eth0" {
			t.Error("DNS escaped captured direct-IP link scope")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("DNS socket has no deadline")
		}
		*calls = append(*calls, network)
		client, upstream := net.Pipe()
		go func() {
			defer func() { _ = upstream.Close() }()
			var request []byte
			if network == "tcp" {
				var header [2]byte
				if _, err := io.ReadFull(upstream, header[:]); err != nil {
					return
				}
				request = make([]byte, int(binary.BigEndian.Uint16(header[:])))
				if _, err := io.ReadFull(upstream, request); err != nil {
					return
				}
			} else {
				buf := make([]byte, 4096)
				n, err := upstream.Read(buf)
				if err != nil {
					return
				}
				request = buf[:n]
			}
			response := answer(network, request)
			if network == "tcp" {
				framed := make([]byte, len(response)+2)
				binary.BigEndian.PutUint16(framed, uint16(len(response)))
				copy(framed[2:], response)
				response = framed
			}
			_, _ = upstream.Write(response)
		}()
		return client, nil
	}
}

func underlayDNSReply(t *testing.T, request []byte) *dnsmessage.Message {
	t.Helper()
	var query dnsmessage.Message
	if err := query.Unpack(request); err != nil {
		t.Fatal(err)
	}
	return &dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionAvailable: true}, Questions: query.Questions, Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 80}}}}}
}

func TestUnderlayDNSBoundQueriesAndTCPTruncation(t *testing.T) {
	for _, scenario := range []string{"valid", "single_label", "truncated", "wrong_id", "wrong_question", "wrong_owner", "wrong_class", "rcode", "not_response", "cname", "cycle", "tcp_truncated"} {
		t.Run(scenario, func(t *testing.T) {
			var calls []string
			host := "control.example"
			if scenario == "single_label" {
				host = "control"
			}
			dial := underlayDNSPipeDial(t, func(network string, request []byte) []byte {
				reply := underlayDNSReply(t, request)
				if reply.Questions[0].Name.String() != host+"." {
					t.Error("query was not the exact absolute authorized name")
				}
				switch scenario {
				case "truncated":
					reply.Truncated = network == "udp"
					if reply.Truncated {
						reply.Answers = nil
					}
				case "tcp_truncated":
					reply.Truncated = true
					reply.Answers = nil
				case "wrong_id":
					reply.ID++
				case "wrong_question":
					reply.Questions[0].Name, _ = dnsmessage.NewName("other.example.")
				case "wrong_owner":
					reply.Answers[0].Header.Name, _ = dnsmessage.NewName("other.example.")
				case "wrong_class":
					reply.Answers[0].Header.Class = dnsmessage.ClassCHAOS
				case "rcode":
					reply.RCode = dnsmessage.RCodeNameError
				case "not_response":
					reply.Response = false
				case "cname", "cycle":
					alias, _ := dnsmessage.NewName("edge.example.")
					if scenario == "cycle" {
						alias = reply.Questions[0].Name
					}
					reply.Answers[0].Header.Name = alias
					reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: reply.Questions[0].Name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET}, Body: &dnsmessage.CNAMEResource{CNAME: alias}})
				}
				raw, err := reply.Pack()
				if err != nil {
					t.Error(err)
				}
				return raw
			}, &calls)
			r := newUnderlayDNSResolver(underlayDNSTestSource(), dial)
			got, err := r.lookup(t.Context(), host, "tcp4")
			valid := scenario == "valid" || scenario == "single_label" || scenario == "truncated" || scenario == "cname"
			if valid {
				if err != nil || len(got) != 1 || got[0].String() != "192.0.2.80" {
					t.Fatal("valid bound DNS reply rejected", got, err)
				}
			} else if err == nil || len(got) != 0 {
				t.Fatal("unbound/incomplete answer accepted", got)
			}
			want := []string{"udp"}
			if scenario == "truncated" || scenario == "tcp_truncated" {
				want = append(want, "tcp")
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatal("unexpected DNS transport or fallback", calls)
			}
		})
	}
}

func TestUnderlayDNSCancellationClosesActiveExchange(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	calls := 0
	dial := func(context.Context, string, netip.AddrPort, underlayDNSLink) (net.Conn, error) {
		calls++
		client, server := net.Pipe()
		go func() {
			defer close(done)
			defer func() { _ = server.Close() }()
			request := make([]byte, 4096)
			if _, err := server.Read(request); err != nil {
				return
			}
			cancel()
			// Wait for cancellation to close the client side, without sending an answer.
			_, _ = server.Read(request)
		}()
		return client, nil
	}
	r := newUnderlayDNSResolver(underlayDNSTestSource(), dial)
	addresses, err := r.lookup(ctx, "control.example", "tcp4")
	if !errors.Is(err, context.Canceled) || len(addresses) != 0 || calls != 1 {
		t.Fatal("cancelled exchange escaped or retried", addresses, err, calls)
	}
	<-done
}

func TestMarkedUnderlayHostAuthorizationAndSourceFreshness(t *testing.T) {
	for _, scenario := range []string{"foreign", "source_missing", "source_policy", "changed_after_dns", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			source := underlayDNSTestSource()
			if scenario == "source_missing" {
				source = nil
			}
			if scenario == "source_policy" {
				source.Links[0].DNSOverTLS = "yes"
			}
			marks := 0
			d := markedUnderlayDialer(51820, func(syscall.RawConn, uint32) error { marks++; return nil }, source, []string{"control.example"})
			var calls []string
			d.resolver.socketDial = underlayDNSPipeDial(t, func(_ string, request []byte) []byte {
				raw, err := underlayDNSReply(t, request).Pack()
				if err != nil {
					t.Error(err)
				}
				return raw
			}, &calls)
			checks := 0
			d.current = func(context.Context) error {
				checks++
				if scenario == "changed_after_dns" && checks > 1 {
					return errors.New("source changed")
				}
				return nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			host := "control.example:443"
			if scenario == "foreign" {
				host = "foreign.example:443"
			}
			conn, err := d.DialContext(ctx, "tcp4", host)
			if conn != nil {
				_ = conn.Close()
			}
			if err == nil || conn != nil || marks != 0 {
				t.Fatal("unauthorized/stale source reached transport", err)
			}
			if scenario != "changed_after_dns" && len(calls) != 0 {
				t.Fatal("rejection leaked DNS query", calls)
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
		})
	}
}
