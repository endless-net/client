//go:build windows

package dataplane

import (
	"context"
	"errors"
	"testing"

	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/wintundriver"
	"golang.org/x/sys/windows"
)

type factoryTUN struct {
	*inertTUN
	luid       uint64
	closes     int
	closeErr   error
	afterClose func()
}

func (t *factoryTUN) LUID() uint64 { return t.luid }
func (t *factoryTUN) Close() error {
	t.closes++
	_ = t.inertTUN.Close()
	if t.afterClose != nil {
		t.afterClose()
	}
	return t.closeErr
}

type mockTUNAPI struct {
	deviceMissing                        bool
	deviceWithoutMIB                     bool
	deviceErr                            error
	rows                                 []adapterIdentity
	row                                  adapterIdentity
	device                               *factoryTUN
	creates, reads                       int
	createErr, identityErr, inventoryErr error
	onCreate                             func()
}

func (a *mockTUNAPI) devicePresent(ctx context.Context, key windows.GUID, _ int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if a.deviceErr != nil {
		return false, a.deviceErr
	}
	if a.deviceWithoutMIB {
		return true, nil
	}
	for _, row := range a.rows {
		if row.GUID == key && !a.deviceMissing {
			return true, nil
		}
	}
	return false, nil
}

func TestWindowsTUNCleanupDistinguishesPhantomMIBAndDevice(t *testing.T) {
	f, api, prepared := tunFactoryFixture(t)
	device, err := f.Create(context.Background(), prepared, 1420, func(context.Context, state.AdapterRecord) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	api.rows = []adapterIdentity{api.row}
	api.deviceMissing = true
	if err := f.ConfirmAbsent(context.Background(), device.Record()); err != nil {
		t.Fatal("removed device with residual owned MIB prevented cleanup", err)
	}
	api.rows = nil
	api.deviceWithoutMIB = true
	if err := f.ConfirmAbsent(context.Background(), device.Record()); !errors.Is(err, ErrAdapterCleanupRequired) {
		t.Fatal("missing MIB hid a still-installed device", err)
	}
	api.deviceWithoutMIB = false
	api.rows = []adapterIdentity{api.row}
	api.deviceErr = ErrAdapterPermission
	if err := f.ConfirmAbsent(context.Background(), device.Record()); !errors.Is(err, ErrAdapterPermission) {
		t.Fatal("unknown device inventory treated as absence", err)
	}
	api.deviceErr = nil
	api.rows[0].GUID = windows.GUID{}
	if err := f.ConfirmAbsent(context.Background(), device.Record()); !errors.Is(err, ErrAdapterOwnership) {
		t.Fatal("PnP absence bypassed recycled LUID ownership", err)
	}
	if api.creates != 1 || api.device.closes != 0 {
		t.Fatal("absence query changed a native adapter")
	}
}

func (a *mockTUNAPI) interfaces() ([]adapterIdentity, error) {
	a.reads++
	return append([]adapterIdentity(nil), a.rows...), a.inventoryErr
}
func (a *mockTUNAPI) identity(uint64) (adapterIdentity, error) { return a.row, a.identityErr }
func (a *mockTUNAPI) create(name string, key windows.GUID, _ int) (nativeTUNDevice, error) {
	a.creates++
	if name != a.row.Name || key != a.row.GUID {
		return nil, ErrConfiguration
	}
	if a.onCreate != nil {
		a.onCreate()
	}
	if a.createErr != nil {
		return nil, a.createErr
	}
	return a.device, nil
}

func tunFactoryFixture(t *testing.T) (*WindowsTUNFactory, *mockTUNAPI, state.AdapterRecord) {
	t.Helper()
	prepared, err := PreparedWindowsAdapter(policySession)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := canonicalGUID(policySession)
	api := &mockTUNAPI{row: adapterIdentity{GUID: key, LUID: 12345, Index: 17, Name: prepared.Name, Type: 53}, device: &factoryTUN{inertTUN: newInertTUN(), luid: 12345}}
	f := &WindowsTUNFactory{api: api, load: func() error { return nil }, maxInterfaces: 16}
	return f, api, prepared
}

func TestWindowsTUNFactoryOrdersJournalAndTransfersOriginalHandle(t *testing.T) {
	f, api, prepared := tunFactoryFixture(t)
	var receipts []state.AdapterRecord
	device, err := f.Create(context.Background(), prepared, 1420, func(_ context.Context, r state.AdapterRecord) error {
		if len(receipts) == 0 && (r != prepared || api.creates != 0) || len(receipts) == 1 && (r.LUID != 12345 || r.Fingerprint != adapterProof(api.row) || api.creates != 1) {
			t.Fatal("native create/receipt preceded durable preparation")
		}
		receipts = append(receipts, r)
		return nil
	})
	if err != nil || len(receipts) != 2 || device.Record() != receipts[1] || api.device.closes != 0 || api.reads != 2 {
		t.Fatal("factory did not transfer acknowledged creation", err)
	}
	api.rows = []adapterIdentity{api.row}
	if err := f.ConfirmAbsent(context.Background(), device.Record()); !errors.Is(err, ErrAdapterCleanupRequired) {
		t.Fatal("live adapter called absent", err)
	}
	if err := device.Close(); err != nil || device.Close() != nil || api.device.closes != 1 {
		t.Fatal("original handle not closed once", err)
	}
	// SDK close success alone is not native absence evidence.
	if err := f.ConfirmAbsent(context.Background(), device.Record()); !errors.Is(err, ErrAdapterCleanupRequired) {
		t.Fatal("SDK close bypassed observed cleanup", err)
	}
	api.rows = nil
	if err := f.ConfirmAbsent(context.Background(), device.Record()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Create(context.Background(), prepared, 1420, func(context.Context, state.AdapterRecord) error { return nil }); !errors.Is(err, ErrAdapterUncertain) || api.creates != 1 {
		t.Fatal("same factory repeated a mutation", err)
	}
}

func TestWindowsTUNFactoryNeverAdoptsConflictingOrLateInterface(t *testing.T) {
	for _, late := range []bool{false, true} {
		for _, byName := range []bool{false, true} {
			f, api, prepared := tunFactoryFixture(t)
			conflict := api.row
			if byName {
				conflict.GUID = windows.GUID{}
			}
			if !late {
				api.rows = []adapterIdentity{conflict}
			}
			_, err := f.Create(context.Background(), prepared, 1420, func(context.Context, state.AdapterRecord) error {
				if late {
					api.rows = []adapterIdentity{conflict}
				}
				return nil
			})
			if !errors.Is(err, ErrAdapterConflict) || api.creates != 0 || api.device.closes != 0 {
				t.Fatal("same name/GUID reused or foreign object closed", err)
			}
		}
	}
}

func TestWindowsTUNFactoryStoreAndCreateFailuresNeverExposeDevice(t *testing.T) {
	for _, failure := range []string{"prepare", "receipt", "create", "identity", "cancel-create", "cancel-receipt", "wrong-guid", "zero-luid", "inventory", "limit", "loader"} {
		t.Run(failure, func(t *testing.T) {
			f, api, prepared := tunFactoryFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch failure {
			case "create":
				api.createErr = windows.ERROR_ACCESS_DENIED
			case "identity":
				api.identityErr = windows.ERROR_ACCESS_DENIED
			case "wrong-guid":
				api.onCreate = func() { api.row.GUID = windows.GUID{} }
			case "zero-luid":
				api.device.luid = 0
			case "cancel-create":
				api.onCreate = cancel
			case "inventory":
				api.inventoryErr = windows.ERROR_ACCESS_DENIED
			case "limit":
				f.maxInterfaces = 1
				api.rows = []adapterIdentity{{}, {}}
			case "loader":
				f.load = func() error { return wintundriver.ErrDriver }
			}
			device, err := f.Create(ctx, prepared, 1420, func(_ context.Context, r state.AdapterRecord) error {
				if failure == "prepare" && r.Phase == state.EffectPrepared || failure == "receipt" && r.Phase == state.EffectApplied {
					return state.ErrUncertain
				}
				if failure == "cancel-receipt" && r.Phase == state.EffectApplied {
					cancel()
				}
				return nil
			})
			if err == nil || device != nil {
				t.Fatal("failed/late create exposed a TUN")
			}
			if api.creates == 1 && api.createErr == nil && api.device.closes != 1 {
				t.Fatal("owned handle leaked after failed receipt")
			}
			if failure == "prepare" && api.creates != 0 {
				t.Fatal("SDK mutation after lost prepare ACK")
			}
		})
	}
}

func TestWindowsTUNCleanupUnknownProofCannotDeleteAndCloseFailureIsTerminal(t *testing.T) {
	f, api, prepared := tunFactoryFixture(t)
	api.rows = []adapterIdentity{api.row}
	if err := f.ConfirmAbsent(context.Background(), prepared); !errors.Is(err, ErrAdapterUncertain) || api.creates != 0 || api.device.closes != 0 {
		t.Fatal("prepared intent adopted native object", err)
	}
	api.rows = nil
	device, err := f.Create(context.Background(), prepared, 1420, func(context.Context, state.AdapterRecord) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	api.rows = []adapterIdentity{api.row}
	api.rows[0].LUID++
	if err := f.ConfirmAbsent(context.Background(), device.Record()); !errors.Is(err, ErrAdapterOwnership) {
		t.Fatal("recreated GUID adopted", err)
	}
	api.device.closeErr = errors.New("raw SDK close detail")
	first, second := device.Close(), device.Close()
	if first != ErrAdapterUncertain || second != ErrAdapterUncertain || api.device.closes != 1 {
		t.Fatal("uncertain close silently retried/leaked detail")
	}
}
