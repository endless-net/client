//go:build windows

package clientapp

import (
	"net/netip"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
)

// ConnectionConfig has no defaults: the announced address is explicit discovery
// input, never inferred from a local NIC or an unverified NAT mapping.
type ConnectionConfig struct {
	Endpoint               string        `env:"ENDPOINT,required"`
	ListenPort             uint16        `env:"LISTEN_PORT,required"`
	MTU                    int           `env:"MTU,required"`
	RouteMetric            uint32        `env:"ROUTE_METRIC,required"`
	MaxPeers               int           `env:"MAX_PEERS,required"`
	MaxRules               int           `env:"MAX_RULES,required"`
	MaxProtected           int           `env:"MAX_PROTECTED,required"`
	MaxInventoryRows       int           `env:"MAX_INVENTORY_ROWS,required"`
	MaxInterfaces          int           `env:"MAX_INTERFACES,required"`
	MaxQueuedMaps          int           `env:"MAX_QUEUED_MAPS,required"`
	MaxMapBytes            int           `env:"MAX_MAP_BYTES,required"`
	MaxStreamRecords       int           `env:"MAX_STREAM_RECORDS,required"`
	MaxCalls               int           `env:"MAX_CALLS,required"`
	MaxStreams             int           `env:"MAX_STREAMS,required"`
	StreamDuration         time.Duration `env:"STREAM_DURATION,required"`
	ReadIdleTimeout        time.Duration `env:"READ_IDLE_TIMEOUT,required"`
	EndpointTTL            time.Duration `env:"ENDPOINT_TTL,required"`
	RenewalInterval        time.Duration `env:"RENEWAL_INTERVAL,required"`
	RetryInterval          time.Duration `env:"RETRY_INTERVAL,required"`
	ProbeInterval          time.Duration `env:"PROBE_INTERVAL,required"`
	PollInterval           time.Duration `env:"POLL_INTERVAL,required"`
	CleanupTimeout         time.Duration `env:"CLEANUP_TIMEOUT,required"`
	DADPollInterval        time.Duration `env:"DAD_POLL_INTERVAL,required"`
	TransactionWait        time.Duration `env:"TRANSACTION_WAIT,required"`
	ApplyTimeout           time.Duration `env:"APPLY_TIMEOUT,required"`
	RelayDialTimeout       time.Duration `env:"RELAY_DIAL_TIMEOUT,required"`
	RelayIOTimeout         time.Duration `env:"RELAY_IO_TIMEOUT,required"`
	RelayHeartbeatInterval time.Duration `env:"RELAY_HEARTBEAT_INTERVAL,required"`
}

func (c Config) connectionHTTP() connectivity.HTTPConfig {
	d := c.ConnectionConfig
	return connectivity.HTTPConfig{CallTimeout: c.CallTimeout, StreamDuration: d.StreamDuration, ReadIdleTimeout: d.ReadIdleTimeout, MaxRequestBytes: c.MaxRequestBytes, MaxResponseBytes: d.MaxMapBytes, MaxRecords: d.MaxStreamRecords, MaxCalls: d.MaxCalls, MaxStreams: d.MaxStreams}
}

func (c Config) connectionDriver(clock connectivity.Clock, announcements control.Announcements) WindowsConnectionConfig {
	d := c.ConnectionConfig
	endpoint, _ := netip.ParseAddrPort(d.Endpoint) // Validate rejects invalid/noncanonical input.
	return WindowsConnectionConfig{
		Verifier: connectivity.VerifierConfig{Clock: clock, Announcements: announcements, Now: time.Now, CallTimeout: c.CallTimeout, MaxBytes: d.MaxMapBytes, MaxPeers: d.MaxPeers, MaxRules: d.MaxRules, MaxCredentialBytes: c.MaxCredentialBytes},
		Native:   dataplane.WindowsSessionConfig{MaxPeers: d.MaxPeers, MaxGrants: d.MaxRules, MaxProtected: d.MaxProtected, MaxInventoryRows: d.MaxInventoryRows, MaxInterfaces: d.MaxInterfaces, MTU: d.MTU, ListenPort: d.ListenPort, RouteMetric: d.RouteMetric, DADPollInterval: d.DADPollInterval, TransactionWait: d.TransactionWait, ApplyTimeout: d.ApplyTimeout, RelayDialTimeout: d.RelayDialTimeout, RelayIOTimeout: d.RelayIOTimeout, RelayHeartbeatInterval: d.RelayHeartbeatInterval, ProbeInterval: d.ProbeInterval, RelayRetryInterval: d.RetryInterval},
		Endpoint: endpoint, EndpointTTL: d.EndpointTTL, RenewalInterval: d.RenewalInterval, RetryInterval: d.RetryInterval, ProbeInterval: d.ProbeInterval, WriteBudget: c.WriteBudget, MaxQueuedMaps: d.MaxQueuedMaps,
	}
}

func (c Config) validateConnection() error {
	d := c.ConnectionConfig
	driver := c.connectionDriver(nil, nil)
	if c.connectionHTTP().Validate() != nil || driver.validateSettings() != nil || driver.Endpoint.String() != d.Endpoint || d.PollInterval < time.Millisecond || d.PollInterval > 10*time.Second || d.CleanupTimeout < time.Millisecond || d.CleanupTimeout > 10*time.Second || d.CleanupTimeout >= c.StopTimeout {
		return ErrConfig
	}
	return nil
}
