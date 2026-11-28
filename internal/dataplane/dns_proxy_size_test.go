//go:build windows || linux

package dataplane

import (
	"bytes"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestDNSUDPReplyRespectsIPv4PayloadLimit(t *testing.T) {
	q := dnsmessage.Question{Name: dnsmessage.MustNewName("large.corp.example."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	opt := dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("."), Class: 65535}, Body: &dnsmessage.OPTResource{}}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: 123}, Questions: []dnsmessage.Question{q}, Additionals: []dnsmessage.Resource{opt}}
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	reply := query
	reply.Response = true
	reply.Authoritative = true
	padding := &dnsmessage.OPTResource{Options: []dnsmessage.Option{{Code: 12}}}
	reply.Additionals = []dnsmessage.Resource{{Header: opt.Header, Body: padding}}
	base, err := reply.Pack()
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{65507, 65508, 65535} {
		padding.Options[0].Data = make([]byte, size-len(base))
		full, err := reply.Pack()
		if err != nil || len(full) != size {
			t.Fatalf("construct %d-byte response: length %d, error %v", size, len(full), err)
		}
		result := dnsUDPReply(wire, full)
		if size == 65507 {
			if !bytes.Equal(result, full) {
				t.Fatal("largest deliverable reply changed")
			}
			continue
		}
		var truncated dnsmessage.Message
		if err := truncated.Unpack(result); err != nil || !truncated.Truncated || !truncated.Authoritative || truncated.ID != query.ID || len(truncated.Questions) != 1 || truncated.Questions[0] != q || len(result) > 512 {
			t.Fatalf("oversized response did not signal a TCP retry: size %d, returned %d bytes", size, len(result))
		}
	}
}
