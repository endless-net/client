package state

import (
	"bytes"
	"encoding/hex"
	"net/netip"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/endless-net/client/internal/enrollment"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type DesiredIntent string

const (
	IntentConnected    DesiredIntent = "connected"
	IntentDisconnected DesiredIntent = "disconnected"
)

type EffectPhase string

const (
	EffectPrepared EffectPhase = "prepared"
	EffectApplied  EffectPhase = "applied"
	EffectRemoving EffectPhase = "removing"
)

// EffectStamp records uncertainty as prepared/removing. Fingerprint is a SHA256
// of the adapter's canonical stable OS attributes, excluding natural counters.
// Cleanup must compare the actual object to this proof before removing it. A
// missing proof never permits deleting an object merely by a familiar name.
type EffectStamp struct {
	Phase       EffectPhase
	Fingerprint string
}

type AdapterRecord struct {
	ID, Name string
	LUID     uint64
	EffectStamp
}
type AddressRecord struct {
	Prefix string
	EffectStamp
	// This local /32 row is an intrinsic stack-created effect of the prepared
	// address operation. Capturing its measured receipt is not a new mutation.
	AutomaticRoute *RouteRecord `json:",omitempty"`
}
type RouteRecord struct {
	Destination, NextHop string
	Metric               uint32
	EffectStamp
}
type FilterRecord struct {
	Key string
	ID  uint64
	EffectStamp
	// Only deny effects carry recovery descriptors; no cached permit is
	// reconstructed as authority after process loss.
	GuardAddress string `json:",omitempty"`
	GuardLocal   bool   `json:",omitempty"`
	GuardInbound bool   `json:",omitempty"`
}

// ResourceJournal identifies one owned adapter session. Generation can precede
// the latest intent after Disconnect: pending cleanup is never erased by a new
// intent or restart. It contains no private key, credential or access grant.
type ResourceJournal struct {
	ID         string
	Generation uint64
	Adapter    *AdapterRecord
	Addresses  []AddressRecord
	Routes     []RouteRecord
	Filters    []FilterRecord
}

type ConnectionState struct {
	Intent               DesiredIntent
	Generation           uint64
	AcceptedAt, Deadline time.Time
	// Persist before publishing each new endpoint lease. Replaying a pending
	// announcement must reuse its generation; restart cannot reset the counter.
	EndpointGeneration uint64
	Journal            *ResourceJournal
	MapCursor          *MapCursor         `json:",omitempty"`
	EndpointRequest    *EndpointRequest   `json:",omitempty"`
	Request            *ConnectionRequest `json:",omitempty"`
}

// ConnectionRequest identifies an accepted IPC attempt using the published
// mutation preconditions. It contains no caller role, credential or grant.
type ConnectionRequest struct {
	InstanceID string
	Revision   uint64
	// Same-daemon timing evidence only; never an access grant after restart.
	ClockEpoch          string        `json:",omitempty"`
	Uptime, UpperUptime time.Duration `json:",omitempty"`
}

// EndpointRequest freezes the exact credential-free producer request before
// dispatch. Sent is persisted before HTTP; a missing receipt then means an
// unresolved outcome, not permission to silently change the same generation.
type EndpointRequest struct {
	IntentGeneration uint64
	Request          []byte
	Sent             bool
	Receipt          *coordinator.UpdateNodeEndpointResponse `json:",omitempty"`
}

func validEndpointRequest(p Profile) bool {
	c := p.Connection
	pending := c.EndpointRequest
	if pending == nil {
		return true
	}
	if pending.IntentGeneration == 0 || pending.IntentGeneration > c.Generation || len(pending.Request) == 0 {
		return false
	}
	r, err := coordinator.DecodeUpdateNodeEndpointRequest(pending.Request, len(pending.Request))
	if err != nil || r.Generation != c.EndpointGeneration || r.Transport != nil && r.Transport.IntentGeneration != pending.IntentGeneration {
		return false
	}
	want := "offline"
	if c.Intent == IntentConnected {
		want = "online"
	}
	if pending.IntentGeneration == c.Generation && r.Status != want {
		return false
	}
	if pending.Receipt != nil {
		return pending.Sent && coordinator.ValidateEndpointReceipt(r, *pending.Receipt, p.Attempt.NodeID, p.Attempt.NetworkID) == nil && !pending.Receipt.ExpiresAt.After(p.Attempt.CredentialEnds)
	}
	return true
}

func endpointRequestUpdate(before, after *ConnectionState) bool {
	b, a := before.EndpointRequest, after.EndpointRequest
	if a == nil {
		return b == nil
	}
	request, err := coordinator.DecodeUpdateNodeEndpointRequest(a.Request, len(a.Request))
	if err != nil {
		return false
	}
	if b == nil {
		return !a.Sent && a.Receipt == nil && a.IntentGeneration == after.Generation
	}
	previous, err := coordinator.DecodeUpdateNodeEndpointRequest(b.Request, len(b.Request))
	if err != nil {
		return false
	}
	if request.Generation != previous.Generation {
		return request.Generation > previous.Generation && !a.Sent && a.Receipt == nil && a.IntentGeneration == after.Generation
	}
	if b.IntentGeneration != a.IntentGeneration || !bytes.Equal(b.Request, a.Request) || b.Sent && !a.Sent {
		return false
	}
	if (!b.Sent && a.Sent || b.Receipt == nil && a.Receipt != nil) && (a.IntentGeneration != after.Generation || before.Generation != after.Generation) {
		return false
	}
	if !b.Sent && a.Receipt != nil {
		return false
	}
	if b.Receipt != nil {
		return a.Receipt != nil && reflect.DeepEqual(b.Receipt, a.Receipt)
	}
	return true
}

// MapCursor is an anti-rollback high-water mark, not a cached access grant.
// Restart still requires a fresh authenticated map before enabling traffic.
type MapCursor struct {
	Revision    coordinator.MapRevision
	PayloadHash string
	IssuedAt    time.Time
}

func validCursor(c *MapCursor) bool {
	return c == nil || c.Revision.Network > 0 && c.Revision.Global == 0 && fingerprint(c.PayloadHash) && !c.IssuedAt.IsZero()
}

func cursorUpdate(before, after *MapCursor) bool {
	if !validCursor(after) {
		return false
	}
	if before == nil {
		return true
	}
	if after == nil || after.Revision.Network < before.Revision.Network || after.IssuedAt.Before(before.IssuedAt) {
		return false
	}
	if before.PayloadHash == after.PayloadHash {
		return before.Revision == after.Revision && before.IssuedAt.Equal(after.IssuedAt)
	}
	return before.Revision != after.Revision || after.IssuedAt.After(before.IssuedAt)
}

func sameCursor(before, after *MapCursor) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	return before.Revision == after.Revision && before.PayloadHash == after.PayloadHash && before.IssuedAt.Equal(after.IssuedAt)
}

func (c *ConnectionState) AcceptIntent(intent DesiredIntent, now time.Time) (bool, error) {
	if c == nil || now.IsZero() || intent != IntentConnected && intent != IntentDisconnected {
		return false, ErrCorrupt
	}
	if c.Intent == intent {
		return false, nil
	}
	if c.Request != nil {
		return false, ErrCorrupt
	}
	return c.beginIntent(intent, now)
}

// BeginRequestedAttempt is used only for a newly accepted user attempt. The
// controller treats an already running/applied desired intent as idempotent and
// does not call this method to silently restart it. Store.Update verifies that
// request.Revision equals the actual revision consumed by this transition.
func (c *ConnectionState) BeginRequestedAttempt(intent DesiredIntent, now time.Time, request ConnectionRequest) (bool, error) {
	if c == nil || now.IsZero() || !validID(request.InstanceID) || request.Revision == 0 || intent != IntentConnected && intent != IntentDisconnected {
		return false, ErrCorrupt
	}
	if c.Request != nil {
		if *c.Request == request {
			if c.Intent != intent {
				return false, ErrCorrupt
			}
			return false, nil
		}
		if request.Revision <= c.Request.Revision {
			return false, ErrStale
		}
	}
	changed, err := c.beginIntent(intent, now)
	if err == nil {
		c.Request = &request
	}
	return changed, err
}

func (c *ConnectionState) beginIntent(intent DesiredIntent, now time.Time) (bool, error) {
	if c.Generation == ^uint64(0) {
		return false, ErrStale
	}
	if !c.AcceptedAt.IsZero() && now.Before(c.AcceptedAt) {
		return false, ErrStale
	}
	c.Intent = intent
	c.Generation++
	c.AcceptedAt = now.UTC()
	budget := 30 * time.Second
	if intent == IntentDisconnected {
		budget = 10 * time.Second
	}
	c.Deadline = c.AcceptedAt.Add(budget)
	return true, nil
}

func (c *ConnectionState) NextEndpointGeneration() (uint64, error) {
	if c == nil || c.Intent != IntentConnected && c.Intent != IntentDisconnected || c.Generation == 0 {
		return 0, ErrCorrupt
	}
	if c.EndpointGeneration == ^uint64(0) {
		return 0, ErrStale
	}
	c.EndpointGeneration++
	return c.EndpointGeneration, nil
}

func fingerprint(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

func stampValid(stamp EffectStamp) bool {
	if stamp.Phase != EffectPrepared && stamp.Phase != EffectApplied && stamp.Phase != EffectRemoving {
		return false
	}
	return (stamp.Fingerprint == "" && stamp.Phase != EffectApplied) || fingerprint(stamp.Fingerprint)
}

func hostPrefix(value string) bool {
	prefix, err := netip.ParsePrefix(value)
	return err == nil && prefix.String() == value && prefix.Bits() == 32 && usableIPv4(prefix.Addr())
}

func usableIPv4(address netip.Addr) bool {
	return address.Is4() && !address.IsUnspecified() && !address.IsMulticast() && !address.IsLoopback()
}

func validateConnection(profile Profile) error {
	c := profile.Connection
	if c == nil {
		return nil
	}
	if profile.Attempt == nil || profile.Attempt.Phase != enrollment.Registered || profile.Secrets.NodeCredential == "" || c.Generation == 0 || c.AcceptedAt.IsZero() || c.AcceptedAt.Before(profile.Attempt.CreatedAt) {
		return ErrCorrupt
	}
	if c.Request != nil && (!validID(c.Request.InstanceID) || c.Request.Revision == 0) {
		return ErrCorrupt
	}
	if c.Request != nil {
		r := c.Request
		if r.ClockEpoch == "" {
			if r.Uptime != 0 || r.UpperUptime != 0 {
				return ErrCorrupt
			}
		} else if r.ClockEpoch != r.InstanceID || r.Uptime < 0 || r.UpperUptime < r.Uptime || r.Uptime > time.Duration(1<<63-1)-30*time.Second {
			return ErrCorrupt
		}
	}
	budget := 30 * time.Second
	switch c.Intent {
	case IntentConnected:
	case IntentDisconnected:
		budget = 10 * time.Second
	default:
		return ErrCorrupt
	}
	if !c.Deadline.Equal(c.AcceptedAt.Add(budget)) {
		return ErrCorrupt
	}
	if !validCursor(c.MapCursor) {
		return ErrCorrupt
	}
	if !validEndpointRequest(profile) {
		return ErrCorrupt
	}
	j := c.Journal
	if j == nil {
		return nil
	}
	if !validID(j.ID) || j.Generation == 0 || j.Generation > c.Generation {
		return ErrCorrupt
	}
	if j.Adapter == nil {
		if len(j.Addresses)+len(j.Routes)+len(j.Filters) != 0 {
			return ErrCorrupt
		}
		return nil
	}
	a := j.Adapter
	if a.ID != j.ID || a.Name == "" || len(a.Name) > 128 || strings.TrimSpace(a.Name) != a.Name || strings.ContainsFunc(a.Name, unicode.IsControl) || !stampValid(a.EffectStamp) || a.Phase == EffectApplied && a.LUID == 0 {
		return ErrCorrupt
	}
	addresses := make(map[string]bool, len(j.Addresses))
	for _, address := range j.Addresses {
		if a.LUID == 0 || !hostPrefix(address.Prefix) || !stampValid(address.EffectStamp) || addresses[address.Prefix] {
			return ErrCorrupt
		}
		addresses[address.Prefix] = true
		if r := address.AutomaticRoute; r != nil {
			if r.Destination != address.Prefix || r.NextHop != "0.0.0.0" || r.Phase != address.Phase || address.Fingerprint == "" || r.Fingerprint == "" || !stampValid(r.EffectStamp) {
				return ErrCorrupt
			}
		}
	}
	routes := make(map[string]bool, len(j.Routes))
	for _, a := range j.Addresses {
		if a.AutomaticRoute != nil {
			routes[a.AutomaticRoute.Destination] = true
		}
	}
	for _, route := range j.Routes {
		// 005 is device-only: no default/subnet route or routed gateway.
		if a.LUID == 0 || !hostPrefix(route.Destination) || route.NextHop != "0.0.0.0" || !stampValid(route.EffectStamp) || routes[route.Destination] {
			return ErrCorrupt
		}
		routes[route.Destination] = true
	}
	keys, ids := make(map[string]bool, len(j.Filters)), make(map[uint64]bool, len(j.Filters))
	for _, filter := range j.Filters {
		if !validID(filter.Key) || keys[filter.Key] || filter.ID != 0 && ids[filter.ID] || !stampValid(filter.EffectStamp) || filter.Phase == EffectApplied && filter.ID == 0 {
			return ErrCorrupt
		}
		if filter.GuardAddress != "" {
			address, err := netip.ParseAddr(filter.GuardAddress)
			if err != nil || !usableIPv4(address) || address.String() != filter.GuardAddress || filter.Fingerprint == "" {
				return ErrCorrupt
			}
		} else if filter.GuardLocal || filter.GuardInbound {
			return ErrCorrupt
		}
		keys[filter.Key] = true
		if filter.ID != 0 {
			ids[filter.ID] = true
		}
	}
	return nil
}

func stampUpdate(before, after EffectStamp) bool {
	if !stampValid(before) || !stampValid(after) || before.Fingerprint != "" && before.Fingerprint != after.Fingerprint {
		return false
	}
	return before.Phase == after.Phase || before.Phase == EffectPrepared && (after.Phase == EffectApplied || after.Phase == EffectRemoving) || before.Phase == EffectApplied && after.Phase == EffectRemoving
}

func connectionUpdate(before, after *ConnectionState) error {
	if before == nil {
		if after == nil {
			return nil
		}
		if after.Generation != 1 || after.EndpointGeneration != 0 || after.Journal != nil || after.MapCursor != nil || after.EndpointRequest != nil {
			return ErrCorrupt
		}
		return nil
	}
	if after == nil || after.EndpointGeneration < before.EndpointGeneration || after.EndpointGeneration-before.EndpointGeneration > 1 {
		return ErrCorrupt
	}
	if !cursorUpdate(before.MapCursor, after.MapCursor) {
		return ErrCorrupt
	}
	if !endpointRequestUpdate(before, after) {
		return ErrCorrupt
	}
	if !sameCursor(before.MapCursor, after.MapCursor) && (after.Intent != IntentConnected || before.Generation != after.Generation) {
		return ErrCorrupt
	}
	newRequest := false
	if before.Request != nil {
		if after.Request == nil {
			return ErrCorrupt
		}
		newRequest = *before.Request != *after.Request
		if newRequest && after.Request.Revision <= before.Request.Revision {
			return ErrCorrupt
		}
	} else {
		newRequest = after.Request != nil
	}
	if before.Request != nil && !newRequest && before.Intent != after.Intent {
		return ErrCorrupt
	}
	if before.Intent == after.Intent && !newRequest {
		if before.Generation != after.Generation || !before.AcceptedAt.Equal(after.AcceptedAt) || !before.Deadline.Equal(after.Deadline) {
			return ErrCorrupt
		}
	} else if before.Generation == ^uint64(0) || after.Generation != before.Generation+1 || after.AcceptedAt.Before(before.AcceptedAt) {
		return ErrCorrupt
	}
	return journalUpdate(before.Journal, after.Journal, after.Intent == IntentConnected, after.Generation)
}

func connectionRequestRevision(before, after *ConnectionState, revision uint64) bool {
	if after == nil || after.Request == nil {
		return true
	}
	if before != nil && before.Request != nil && *before.Request == *after.Request {
		return true
	}
	return after.Request.Revision == revision
}

func journalUpdate(before, after *ResourceJournal, connected bool, generation uint64) error {
	if before == nil {
		if after == nil {
			return nil
		}
		if !connected || after.Generation != generation || after.Adapter == nil || after.Adapter.Phase != EffectPrepared || after.Adapter.LUID != 0 || len(after.Addresses)+len(after.Routes)+len(after.Filters) != 0 {
			return ErrCorrupt
		}
		return nil
	}
	if after == nil {
		if before.Adapter != nil || len(before.Addresses)+len(before.Routes)+len(before.Filters) != 0 {
			return ErrCorrupt
		}
		return nil
	}
	if before.ID != after.ID || before.Generation != after.Generation {
		return ErrCorrupt
	}
	adding := connected && after.Generation == generation
	if before.Adapter == nil {
		// An emptied journal is retired before a new session is created.
		if after.Adapter != nil {
			return ErrCorrupt
		}
	} else if after.Adapter == nil {
		if before.Adapter.Phase != EffectRemoving || len(after.Addresses)+len(after.Routes)+len(after.Filters) != 0 {
			return ErrCorrupt
		}
	} else {
		b, a := *before.Adapter, *after.Adapter
		if b.ID != a.ID || b.Name != a.Name || b.LUID != 0 && b.LUID != a.LUID || !stampUpdate(b.EffectStamp, a.EffectStamp) {
			return ErrCorrupt
		}
	}
	if !addressUpdates(before.Addresses, after.Addresses, adding) || !routeUpdates(before.Routes, after.Routes, adding) || !filterUpdates(before.Filters, after.Filters, adding) {
		return ErrCorrupt
	}
	return nil
}

func addressUpdates(before, after []AddressRecord, adding bool) bool {
	old := make(map[string]AddressRecord, len(before))
	for _, value := range before {
		old[value.Prefix] = value
	}
	for _, value := range after {
		if previous, exists := old[value.Prefix]; exists {
			if !stampUpdate(previous.EffectStamp, value.EffectStamp) {
				return false
			}
			if previous.AutomaticRoute != nil {
				r, next := previous.AutomaticRoute, value.AutomaticRoute
				if next == nil || r.Destination != next.Destination || r.NextHop != next.NextHop || r.Metric != next.Metric || !stampUpdate(r.EffectStamp, next.EffectStamp) {
					return false
				}
			}
			delete(old, value.Prefix)
		} else if !adding || value.Phase != EffectPrepared || value.AutomaticRoute != nil {
			return false
		}
	}
	for _, value := range old {
		if value.Phase != EffectRemoving {
			return false
		}
	}
	return true
}
func routeUpdates(before, after []RouteRecord, adding bool) bool {
	old := make(map[string]RouteRecord, len(before))
	for _, value := range before {
		old[value.Destination] = value
	}
	for _, value := range after {
		if previous, exists := old[value.Destination]; exists {
			if previous.NextHop != value.NextHop || previous.Metric != value.Metric || !stampUpdate(previous.EffectStamp, value.EffectStamp) {
				return false
			}
			delete(old, value.Destination)
		} else if !adding || value.Phase != EffectPrepared {
			return false
		}
	}
	for _, value := range old {
		if value.Phase != EffectRemoving {
			return false
		}
	}
	return true
}
func filterUpdates(before, after []FilterRecord, adding bool) bool {
	old := make(map[string]FilterRecord, len(before))
	for _, value := range before {
		old[value.Key] = value
	}
	for _, value := range after {
		if previous, exists := old[value.Key]; exists {
			if previous.ID != 0 && previous.ID != value.ID || !stampUpdate(previous.EffectStamp, value.EffectStamp) || previous.GuardAddress != value.GuardAddress || previous.GuardLocal != value.GuardLocal || previous.GuardInbound != value.GuardInbound {
				return false
			}
			delete(old, value.Key)
		} else if !adding || value.Phase != EffectPrepared || value.ID != 0 {
			return false
		}
	}
	for _, value := range old {
		if value.Phase != EffectRemoving {
			return false
		}
	}
	return true
}
