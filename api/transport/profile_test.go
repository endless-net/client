package transport_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/endless-net/client/api/transport"
)

func wg() transport.Profile {
	return transport.Profile{Version: 1, ID: "wg", Revision: 9007199254740993, Engine: transport.WireGuard, ProtocolVersion: "1", Prelude: []transport.Datagram{}}
}

func awg() transport.Profile {
	p := wg()
	p.ID, p.Engine, p.ProtocolVersion = "awg", transport.AmneziaWG, "3.1"
	p.AWG = &transport.AWG{Headers: []transport.Range{{Min: 100, Max: 199}, {Min: 200, Max: 299}, {Min: 300, Max: 399}, {Min: 400, Max: 499}}, Padding: []uint16{12, 16, 20, 24}, HeaderProtectionKey: bytes.Repeat([]byte{9}, 32), ContentPadding: transport.Range{Min: 0, Max: 32}, RandomTrailers: true, DisableCookies: true}
	p.Prelude = []transport.Datagram{{DelayMS: 10, Segments: []transport.Segment{{Bytes: []byte{1, 2, 3}}, {Random: 16}}}}
	return p
}

func TestCanonicalProfileDigestAndPrecision(t *testing.T) {
	p := wg()
	raw, err := transport.CanonicalProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"version":1,"id":"wg","revision":9007199254740993,"engine":"wireguard","protocol_version":"1","prelude":[]}`
	if string(raw) != expected {
		t.Fatal("canonical profile bytes changed")
	}
	reordered := []byte(` {"prelude":[],"protocol_version":"1","engine":"wireguard","revision":9007199254740993,"id":"w\u0067","version":1} `)
	decoded, err := transport.DecodeProfile(reordered)
	if err != nil || decoded.Revision != p.Revision {
		t.Fatal("strict uint64 precision lost", err)
	}
	first, err := transport.ProfileDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := transport.ProfileDigest(decoded)
	if err != nil || first != second || !transport.ValidDigest(first) {
		t.Fatal("digest depends on JSON spelling", err)
	}
	decoded.Revision++
	changed, err := transport.ProfileDigest(decoded)
	if err != nil || first == changed {
		t.Fatal("revision not covered by digest", err)
	}
}

func TestProfileLimitsAndRequiredSemantics(t *testing.T) {
	for name, modify := range map[string]func(*transport.Profile){
		"unknown version":           func(p *transport.Profile) { p.Version++ },
		"unknown engine":            func(p *transport.Profile) { p.Engine = "script" },
		"unknown protocol":          func(p *transport.Profile) { p.ProtocolVersion = "4" },
		"zero revision":             func(p *transport.Profile) { p.Revision = 0 },
		"bad ID":                    func(p *transport.Profile) { p.ID = "bad\nidentity" },
		"nil prelude":               func(p *transport.Profile) { p.Prelude = nil },
		"WG with AWG":               func(p *transport.Profile) { p.Engine, p.ProtocolVersion = transport.WireGuard, "1" },
		"missing AWG":               func(p *transport.Profile) { p.AWG = nil },
		"wrong header count":        func(p *transport.Profile) { p.AWG.Headers = p.AWG.Headers[:3] },
		"overlapping headers":       func(p *transport.Profile) { p.AWG.Headers[1].Min = p.AWG.Headers[0].Max },
		"reversed headers":          func(p *transport.Profile) { p.AWG.Headers[0].Min = 999 },
		"short header key":          func(p *transport.Profile) { p.AWG.HeaderProtectionKey = p.AWG.HeaderProtectionKey[:31] },
		"zero header key":           func(p *transport.Profile) { clear(p.AWG.HeaderProtectionKey) },
		"padding under protection":  func(p *transport.Profile) { p.AWG.Padding[2] = 11 },
		"padding overflow":          func(p *transport.Profile) { p.AWG.Padding[3] = 1201 },
		"padding addition overflow": func(p *transport.Profile) { p.AWG.ContentPadding.Max = 1201 },
		"reversed padding range":    func(p *transport.Profile) { p.AWG.ContentPadding.Min = 33 },
		"empty segment":             func(p *transport.Profile) { p.Prelude[0].Segments = []transport.Segment{{}} },
		"two primitives":            func(p *transport.Profile) { p.Prelude[0].Segments[0].Random = 1 },
		"datagram overflow":         func(p *transport.Profile) { p.Prelude[0].Segments = []transport.Segment{{Random: 1200}, {Random: 1}} },
		"random overflow":           func(p *transport.Profile) { p.Prelude[0].Segments[1].Random = 1201 },
		"delay sum overflow": func(p *transport.Profile) {
			p.Prelude = []transport.Datagram{{DelayMS: 1501, Segments: []transport.Segment{{Random: 1}}}, {DelayMS: 1500, Segments: []transport.Segment{{Random: 1}}}}
		},
		"too many datagrams": func(p *transport.Profile) {
			for range 16 {
				p.Prelude = append(p.Prelude, p.Prelude[0])
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := awg()
			modify(&p)
			if transport.ValidateProfile(p) == nil {
				t.Fatal("invalid profile accepted")
			}
			if _, err := transport.CanonicalProfile(p); err == nil {
				t.Fatal("invalid profile serialized")
			}
		})
	}
	for _, p := range []transport.Profile{wg(), awg()} {
		raw, err := transport.CanonicalProfile(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transport.DecodeProfile(raw); err != nil {
			t.Fatal("supported profile rejected", err)
		}
	}
}

func TestProfileStrictDecoder(t *testing.T) {
	raw, err := transport.CanonicalProfile(awg())
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"duplicate":            func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"version":1`, 1) },
		"escaped duplicate":    func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"\u0076ersion":1`, 1) },
		"case alias":           func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"VERSION":1`, 1) },
		"missing zero boolean": func(s string) string { return strings.Replace(s, `,"disable_cookies":true`, ``, 1) },
		"nested case alias":    func(s string) string { return strings.Replace(s, `"min":100`, `"min":100,"MIN":100`, 1) },
		"unknown raw CPS": func(s string) string {
			return strings.Replace(s, `"version":1`, `"version":1,"i1":"<r 1000000000>"`, 1)
		},
		"null profile":            func(string) string { return `null` },
		"null optional":           func(s string) string { return strings.Replace(s, `"bytes":"AQID"`, `"bytes":null`, 1) },
		"array instead of base64": func(s string) string { return strings.Replace(s, `"bytes":"AQID"`, `"bytes":[1,2,3]`, 1) },
		"noncanonical base64":     func(s string) string { return strings.Replace(s, `"bytes":"AQID"`, `"bytes":"AR=="`, 1) },
		"null array":              func(s string) string { return strings.Replace(s, `"padding":[12,16,20,24]`, `"padding":null`, 1) },
		"missing key":             func(s string) string { return strings.Replace(s, `"revision":9007199254740993,`, ``, 1) },
		"decimal integer":         func(s string) string { return strings.Replace(s, `"version":1`, `"version":1.0`, 1) },
		"negative unsigned":       func(s string) string { return strings.Replace(s, `"delay_ms":10`, `"delay_ms":-1`, 1) },
		"uint64 overflow":         func(s string) string { return strings.Replace(s, `9007199254740993`, `18446744073709551616`, 1) },
		"trailing value":          func(s string) string { return s + `{}` },
		"present empty primitive": func(s string) string { return strings.Replace(s, `"random":16`, `"random":16,"bytes":""`, 1) },
		"present zero primitive":  func(s string) string { return strings.Replace(s, `"bytes":"AQID"`, `"bytes":"AQID","random":0`, 1) },
		"too deep":                func(string) string { return strings.Repeat("[", 10) + "0" + strings.Repeat("]", 10) },
	} {
		t.Run(name, func(t *testing.T) {
			modified := mutate(string(raw))
			if modified == string(raw) {
				t.Fatal("mutation did not exercise input")
			}
			if _, err := transport.DecodeProfile([]byte(modified)); err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
	if _, err := transport.DecodeProfile(bytes.Repeat([]byte{' '}, transport.MaxProfileBytes+1)); !errors.Is(err, transport.ErrInvalid) {
		t.Fatal("profile input byte limit ignored")
	}
}

func TestCapabilitiesIntersectionAndCanonicalOrder(t *testing.T) {
	w, a := transport.Capability{Engine: transport.WireGuard, ProtocolVersion: "1", ProfileVersion: 1}, transport.Capability{Engine: transport.AmneziaWG, ProtocolVersion: "3.1", ProfileVersion: 1}
	left := transport.Capabilities{Version: 1, Engines: []transport.Capability{w, a}}
	right := transport.Capabilities{Version: 1, Engines: []transport.Capability{w}}
	if !transport.CommonProfile(wg(), left, right) || transport.CommonProfile(awg(), left, right) {
		t.Fatal("one-sided capability treated as mutual")
	}
	x, err := transport.CanonicalCapabilities(left)
	if err != nil || left.Engines[0] != w {
		t.Fatal("canonicalization mutated caller", err)
	}
	left.Engines[0], left.Engines[1] = a, w
	y, err := transport.CanonicalCapabilities(left)
	if err != nil || !bytes.Equal(x, y) {
		t.Fatal("canonical capabilities depend on order", err)
	}
	for _, raw := range [][]byte{[]byte(`{"version":1,"engines":[]}`), []byte(`{"version":1,"engines":[{"engine":"wireguard","protocol_version":"1","profile_version":1},{"engine":"wireguard","protocol_version":"1","profile_version":1}]}`), []byte(`{"version":1,"engines":[{"engine":"amneziawg","protocol_version":"3.1","profile_version":2}]}`)} {
		if _, err := transport.DecodeCapabilities(raw); err == nil {
			t.Fatal("invalid/unsupported capability accepted")
		}
	}
}

func TestPeerProfileSetAndMaximumDatagrams(t *testing.T) {
	profiles := make([]transport.Profile, transport.MaxProfiles)
	for i := range profiles {
		profiles[i] = wg()
		profiles[i].ID = fmt.Sprintf("profile-%d", i)
	}
	if err := transport.ValidateProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	if transport.ValidateProfiles(append(profiles, awg())) == nil {
		t.Fatal("too many peer profiles accepted")
	}
	profiles[1] = profiles[0]
	if transport.ValidateProfiles(profiles) == nil {
		t.Fatal("duplicate profile ID accepted")
	}
	p := awg()
	p.Prelude = make([]transport.Datagram, transport.MaxPrelude)
	for i := range p.Prelude {
		p.Prelude[i] = transport.Datagram{Segments: []transport.Segment{{Random: transport.MaxDatagramBytes}}}
	}
	if _, err := transport.CanonicalProfile(p); err != nil {
		t.Fatal("maximum count/size datagrams rejected", err)
	}
	// A valid datagram may have many small segments, but the serialized profile
	// must still obey its independent byte ceiling.
	for i := range p.Prelude {
		p.Prelude[i].Segments = make([]transport.Segment, transport.MaxDatagramBytes)
		for j := range p.Prelude[i].Segments {
			p.Prelude[i].Segments[j] = transport.Segment{Random: 1}
		}
	}
	if transport.ValidateProfile(p) == nil {
		t.Fatal("serialized profile bound ignored")
	}
}

func TestTransportFixtures(t *testing.T) {
	raw, err := os.ReadFile("fixtures/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Instance, Schema string
		Valid                  bool
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			raw, err := os.ReadFile(c.Instance)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case strings.HasSuffix(c.Schema, "/Capabilities"):
				_, err = transport.DecodeCapabilities(raw)
			case strings.HasSuffix(c.Schema, "/Control"):
				_, err = transport.DecodeControl(raw)
			default:
				_, err = transport.DecodeProfile(raw)
			}
			if (err == nil) != c.Valid {
				t.Fatalf("valid=%v: %v", c.Valid, err)
			}
		})
	}
	if len(cases) == 0 {
		t.Fatal("no producer fixtures exercised")
	}
}

func TestTemplateAndKeyRedaction(t *testing.T) {
	p := awg()
	for _, value := range []any{p, &p, p.AWG, p.Prelude[0], p.Prelude[0].Segments[0]} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			text := fmt.Sprintf(format, value)
			if strings.Contains(text, "AQID") || strings.Contains(text, base64.StdEncoding.EncodeToString(p.AWG.HeaderProtectionKey)) || strings.Contains(text, "HeaderProtectionKey") || strings.Contains(text, "[9 9") {
				t.Fatal("transport parameters leaked into diagnostics")
			}
		}
	}
}
