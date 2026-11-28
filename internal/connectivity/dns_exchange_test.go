package connectivity

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

type dnsTestDialer struct {
	endpoints map[string]string
	calls     []string
}

func (d *dnsTestDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	key := network + "/" + address
	d.calls = append(d.calls, key)
	target, ok := d.endpoints[key]
	if !ok {
		return nil, errors.New("unexpected upstream")
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, target)
}

func TestDNSExchangeConfinesRetriesAndTCPToSelectedZone(t *testing.T) {
	private := netip.MustParseAddr("192.0.2.2")
	backup := netip.MustParseAddr("192.0.2.3")
	primary := netip.MustParseAddr("192.0.2.1")
	routes := DNSRoutes{zones: map[string][]netip.Addr{"corp.example": {private, backup}, "": {primary}}}
	q := dnsmessage.Question{Name: dnsmessage.MustNewName("db.corp.example."), Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET}
	pack := func(h dnsmessage.Header) []byte {
		m := dnsmessage.Message{Header: h, Questions: []dnsmessage.Question{q}}
		wire, err := m.Pack()
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	query := pack(dnsmessage.Header{ID: 734, RecursionDesired: true})
	udp := func(reply []byte) string {
		listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { listener.Close() })
		go func() {
			var buf [512]byte
			_, from, err := listener.ReadFrom(buf[:])
			if err == nil {
				_, _ = listener.WriteTo(reply, from)
			}
		}()
		return listener.LocalAddr().String()
	}
	tcp := func(reply []byte) string {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { listener.Close() })
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			var size [2]byte
			if _, err := io.ReadFull(conn, size[:]); err != nil {
				return
			}
			if _, err := io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint16(size[:]))); err != nil {
				return
			}
			binary.BigEndian.PutUint16(size[:], uint16(len(reply)))
			_, _ = conn.Write(append(size[:], reply...))
		}()
		return listener.Addr().String()
	}
	failed := pack(dnsmessage.Header{ID: 734, Response: true, RCode: dnsmessage.RCodeServerFailure})
	truncated := pack(dnsmessage.Header{ID: 734, Response: true, Truncated: true})
	nx := pack(dnsmessage.Header{ID: 734, Response: true, RCode: dnsmessage.RCodeNameError})
	first, second, stream := "udp/192.0.2.2:53", "udp/192.0.2.3:53", "tcp/192.0.2.3:53"
	t.Run("retry and tcp", func(t *testing.T) {
		dial := &dnsTestDialer{endpoints: map[string]string{first: udp(failed), second: udp(truncated), stream: tcp(nx)}}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		got, err := routes.Exchange(ctx, query, dial)
		if err != nil || !slices.Equal(got, nx) || !slices.Equal(dial.calls, []string{first, second, stream}) {
			t.Fatal("DNS response or upstream sequence", err, dial.calls)
		}
	})
	t.Run("private servers failed", func(t *testing.T) {
		dial := &dnsTestDialer{endpoints: map[string]string{first: udp(failed), second: udp(failed)}}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		_, err := routes.Exchange(ctx, query, dial)
		if !errors.Is(err, ErrDNSUpstream) || !slices.Equal(dial.calls, []string{first, second}) {
			t.Fatal("private failure escaped assigned servers", err, dial.calls)
		}
	})
}
func TestDNSExchangeRejectsMalformedQueriesBeforeDial(t *testing.T) {
	routes := DNSRoutes{zones: map[string][]netip.Addr{"": {netip.MustParseAddr("192.0.2.1")}}}
	dial := &dnsTestDialer{}
	q := dnsmessage.Question{Name: dnsmessage.MustNewName("external.example."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	multiple := dnsmessage.Message{Questions: []dnsmessage.Question{q, q}}
	wire, err := multiple.Pack()
	if err != nil {
		t.Fatal(err)
	}
	for _, packet := range [][]byte{nil, {0}, make([]byte, 12), wire} {
		if _, err := routes.Exchange(t.Context(), packet, dial); !errors.Is(err, ErrDNSQuery) {
			t.Fatalf("invalid packet accepted: %v", err)
		}
	}
	if len(dial.calls) != 0 {
		t.Fatal("invalid question reached upstream")
	}
}
