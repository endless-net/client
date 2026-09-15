package client

import "testing"

func TestEngineControlUnderlayRequiresOwnedExitIdentity(t *testing.T) {
	bound := Config{NodeID: "node", NodeCredential: "synthetic-credential", NetworkID: "network", ControlPlaneURLs: []string{"https://control.example"}}
	engine := &WireGuardEngine{exitGuard: &linuxExitGuard{mark: 51820}, exitConfig: bound}
	// A stopped tunnel has no router mark, but the persistent guard still owns
	// the underlay. Constructing a client must retain marked/no-proxy transport.
	control, err := engine.ControlPlaneHTTPClient(bound)
	if err != nil {
		t.Fatal(err)
	}
	defer control.CloseIdleConnections()
	transport := control.Transport.(*controlUnderlayTransport)
	if transport.base.Proxy != nil || transport.base.DialContext == nil {
		t.Fatal("stopped tunnel lost protected underlay transport")
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"node", func(c *Config) { c.NodeID = "other" }},
		{"credential", func(c *Config) { c.NodeCredential = "other" }},
		{"network", func(c *Config) { c.NetworkID = "other" }},
		{"route_table", func(c *Config) { c.WireGuardRouteTable = "51999" }},
		{"origin", func(c *Config) { c.ControlPlaneURLs = []string{"https://other.example"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := bound
			tc.change(&cfg)
			if control, err := engine.ControlPlaneHTTPClient(cfg); err == nil || control != nil {
				t.Fatal("replacement identity acquired old guard's underlay")
			}
		})
	}
	engine.exitConfig = Config{}
	if control, err := engine.ControlPlaneHTTPClient(bound); err == nil || control != nil {
		t.Fatal("unbound guard authorized control traffic")
	}
	engine.exitGuard = nil
	bound.ExitSelection = &ClientExitSelection{}
	if control, err := engine.ControlPlaneHTTPClient(bound); err == nil || control != nil {
		t.Fatal("unrestored exit used unmarked control transport")
	}
}
