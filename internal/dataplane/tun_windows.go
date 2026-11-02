//go:build windows

package dataplane

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/wintundriver"
	"github.com/tailscale/wireguard-go/tun"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

var (
	ErrAdapter                = errors.New("tunnel adapter failed")
	ErrAdapterConflict        = errors.New("tunnel adapter conflicts with existing interface")
	ErrAdapterOwnership       = errors.New("tunnel adapter ownership changed")
	ErrAdapterUncertain       = errors.New("tunnel adapter outcome uncertain")
	ErrAdapterPermission      = errors.New("tunnel adapter permission denied")
	ErrAdapterLimit           = errors.New("interface inventory limit exceeded")
	ErrAdapterCleanupRequired = errors.New("tunnel adapter cleanup required")
)

type nativeTUNDevice interface {
	tun.Device
	LUID() uint64
}

type adapterIdentity struct {
	GUID             windows.GUID
	LUID             uint64
	Index            uint32
	Name             string
	Type, TunnelType uint32
}

func adapterInfo(row winipcfg.MibIfRow2) adapterIdentity {
	return adapterIdentity{GUID: row.InterfaceGUID, LUID: uint64(row.InterfaceLUID), Index: row.InterfaceIndex, Name: row.Alias(), Type: uint32(row.Type), TunnelType: uint32(row.TunnelType)}
}

type tunAPI interface {
	interfaces() ([]adapterIdentity, error)
	identity(uint64) (adapterIdentity, error)
	create(string, windows.GUID, int) (nativeTUNDevice, error)
}

type nativeTUNAPI struct{}

func (nativeTUNAPI) interfaces() ([]adapterIdentity, error) {
	rows, err := winipcfg.GetIfTable2Ex(winipcfg.MibIfEntryNormal)
	if err != nil {
		return nil, err
	}
	out := make([]adapterIdentity, len(rows))
	for i, row := range rows {
		out[i] = adapterInfo(row)
	}
	return out, nil
}

func (nativeTUNAPI) identity(luid uint64) (adapterIdentity, error) {
	row, err := winipcfg.LUID(luid).Interface()
	if err != nil || row == nil {
		if err == nil {
			err = ErrAdapter
		}
		return adapterIdentity{}, err
	}
	return adapterInfo(*row), nil
}

func (nativeTUNAPI) create(name string, guid windows.GUID, mtu int) (nativeTUNDevice, error) {
	device, err := tun.CreateTUNWithRequestedGUID(name, &guid, mtu)
	if err != nil {
		return nil, err
	}
	owned, ok := device.(nativeTUNDevice)
	if !ok {
		_ = device.Close()
		return nil, ErrAdapterUncertain
	}
	return owned, nil
}

// AdapterJournalWriter must use a protected Store and bind the initiating
// profile/generation. Prepared must create a fresh journal, not adopt/replay an
// existing one; Applied durably acknowledges the measured creation receipt.
type AdapterJournalWriter func(context.Context, state.AdapterRecord) error

// WindowsTUNFactory attempts one creation only. A new factory must not bypass
// the protected writer's refusal of an unresolved existing journal.
type WindowsTUNFactory struct {
	mu            sync.Mutex
	api           tunAPI
	load          func() error
	maxInterfaces int
	used          bool
}

func NewWindowsTUNFactory(maxInterfaces int) (*WindowsTUNFactory, error) {
	if maxInterfaces <= 0 {
		return nil, ErrConfiguration
	}
	return &WindowsTUNFactory{api: nativeTUNAPI{}, load: wintundriver.Load, maxInterfaces: maxInterfaces}, nil
}

func PreparedWindowsAdapter(sessionID string) (state.AdapterRecord, error) {
	if _, err := canonicalGUID(sessionID); err != nil {
		return state.AdapterRecord{}, err
	}
	return state.AdapterRecord{ID: sessionID, Name: "EndlessNet-" + sessionID, EffectStamp: state.EffectStamp{Phase: state.EffectPrepared}}, nil
}

func adapterError(err error) error {
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return ErrAdapterPermission
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
		return ErrAdapterConflict
	}
	return ErrAdapter
}

func adapterProof(row adapterIdentity) string {
	// Interface index can change naturally; it is checked at each policy apply,
	// not used as a durable creation identity or restored as a cached permit.
	row.Index = 0
	return ipProof(row)
}

func (f *WindowsTUNFactory) inventory(ctx context.Context) ([]adapterIdentity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := f.api.interfaces()
	if err != nil {
		return nil, adapterError(err)
	}
	if len(rows) > f.maxInterfaces {
		return nil, ErrAdapterLimit
	}
	return rows, ctx.Err()
}

func (f *WindowsTUNFactory) preflight(ctx context.Context, key windows.GUID, name string) error {
	rows, err := f.inventory(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.GUID == key || strings.EqualFold(row.Name, name) {
			return ErrAdapterConflict
		}
	}
	return nil
}

// Create returns an unconfigured TUN only after protected preparation and receipt
// ACK. It does not set addresses/routes/DNS/MTU policy or construct a WG engine.
// Wintun starts its packet ring, but no packets are consumed or sent by this
// factory. The owner must install guards and the current Lease before activation.
func (f *WindowsTUNFactory) Create(ctx context.Context, prepared state.AdapterRecord, mtu int, write AdapterJournalWriter) (*WindowsTUN, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	expected, err := PreparedWindowsAdapter(prepared.ID)
	if ctx == nil || err != nil || prepared != expected || mtu <= 0 || mtu > 65535 || write == nil || f.api == nil || f.load == nil || f.maxInterfaces <= 0 {
		return nil, ErrConfiguration
	}
	if f.used {
		return nil, ErrAdapterUncertain
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.load(); err != nil {
		return nil, errors.Join(ErrAdapter, wintundriver.ErrDriver)
	}
	key, _ := canonicalGUID(prepared.ID)
	if err := f.preflight(ctx, key, prepared.Name); err != nil {
		return nil, err
	}
	f.used = true // includes a lost prepared-journal ACK, not just SDK calls
	if err := write(ctx, prepared); err != nil {
		return nil, errors.Join(ErrAdapterUncertain, err)
	}
	// Preparation may race another interface creator. Never reuse a familiar
	// name/GUID, even if it appeared while this journal was being persisted.
	if err := f.preflight(ctx, key, prepared.Name); err != nil {
		return nil, err
	}
	device, err := f.api.create(prepared.Name, key, mtu)
	if err != nil {
		if device != nil {
			_ = device.Close()
		}
		return nil, errors.Join(ErrAdapterUncertain, adapterError(err))
	}
	if device == nil {
		return nil, ErrAdapterUncertain
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = device.Close() // original creation handle only, never OpenAdapter
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrAdapterUncertain, err)
	}
	luid := device.LUID()
	if luid == 0 {
		return nil, ErrAdapterUncertain
	}
	row, err := f.api.identity(luid)
	if err != nil {
		return nil, errors.Join(ErrAdapterUncertain, adapterError(err))
	}
	if row.GUID != key || row.LUID != luid || row.Index == 0 || row.Name != prepared.Name {
		return nil, errors.Join(ErrAdapterUncertain, ErrAdapterOwnership)
	}
	receipt := prepared
	receipt.LUID = luid
	receipt.EffectStamp = state.EffectStamp{Phase: state.EffectApplied, Fingerprint: adapterProof(row)}
	if err := write(ctx, receipt); err != nil {
		return nil, errors.Join(ErrAdapterUncertain, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrAdapterUncertain, err)
	}
	transferred = true
	return &WindowsTUN{nativeTUNDevice: device, receipt: receipt}, nil
}

// ConfirmAbsent is cleanup-only. Prepared/unknown receipts may be retired only
// after native absence; they never authorize deleting an existing same-key row.
func (f *WindowsTUNFactory) ConfirmAbsent(ctx context.Context, record state.AdapterRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx == nil || f.api == nil || f.maxInterfaces <= 0 {
		return ErrConfiguration
	}
	key, err := canonicalGUID(record.ID)
	if err != nil {
		return err
	}
	rows, err := f.inventory(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.GUID != key && (record.LUID == 0 || row.LUID != record.LUID) {
			continue
		}
		if record.LUID == 0 || record.Fingerprint == "" {
			return ErrAdapterUncertain
		}
		if row.GUID != key || row.LUID != record.LUID || row.Name != record.Name || adapterProof(row) != record.Fingerprint {
			return ErrAdapterOwnership
		}
		return ErrAdapterCleanupRequired
	}
	return ctx.Err()
}

// WindowsTUN owns the handle returned by CreateAdapter, never an opened/reused
// adapter. Close drops it once, but does not prove native absence or journal
// cleanup. Normal teardown records removing first; error/process loss can leave
// prepared/applied records, which recovery must resolve by native observation.
type WindowsTUN struct {
	nativeTUNDevice
	mu       sync.Mutex
	receipt  state.AdapterRecord
	closed   bool
	closeErr error
}

func (*WindowsTUN) String() string                { return "[Windows tunnel]" }
func (*WindowsTUN) GoString() string              { return "[Windows tunnel]" }
func (t *WindowsTUN) Record() state.AdapterRecord { return t.receipt }

func (t *WindowsTUN) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		if t.nativeTUNDevice.Close() != nil {
			t.closeErr = ErrAdapterUncertain
		}
	}
	return t.closeErr
}
