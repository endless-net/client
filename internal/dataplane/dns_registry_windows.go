package dataplane

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/endless-net/client/internal/state"
	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Only local DNS client rules belong to this service. Group Policy is owned
// by the administrator. Tests use a private HKCU subtree.
type windowsDNSRegistry struct {
	root registry.Key
	path string
}

func localDNSRegistry() windowsDNSRegistry {
	return windowsDNSRegistry{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig`}
}

var (
	ktm               = windows.NewLazySystemDLL("ktmw32.dll")
	createTransaction = ktm.NewProc("CreateTransaction")
	commitTransaction = ktm.NewProc("CommitTransaction")
	dnsAdvapi         = windows.NewLazySystemDLL("advapi32.dll")
	createRuleKey     = dnsAdvapi.NewProc("RegCreateKeyTransactedW")
	openRuleKey       = dnsAdvapi.NewProc("RegOpenKeyTransactedW")
	deleteRuleKey     = dnsAdvapi.NewProc("RegDeleteKeyTransactedW")
	setRuleValue      = dnsAdvapi.NewProc("RegSetValueExW")
)

func registryTransaction(ctx context.Context, run func(windows.Handle) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h, _, err := createTransaction.Call(0, 0, 0, 0, 0, 0, 0)
	if windows.Handle(h) == windows.InvalidHandle {
		return err
	}
	// Closing an uncommitted transaction rolls back its writes.
	defer func() { _ = windows.CloseHandle(windows.Handle(h)) }()
	if err := run(windows.Handle(h)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ok, _, err := commitTransaction.Call(h)
	if ok == 0 {
		return err
	}
	return nil
}

type dnsRegistryValue struct {
	Name string
	Kind uint32
	Data []byte
}

func dnsRuleFingerprint(values []dnsRegistryValue) string {
	raw, _ := json.Marshal(values)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func utf16Bytes(s string) []byte {
	units := utf16.Encode([]rune(s))
	result := make([]byte, 0, len(units)*2)
	for _, v := range units {
		result = binary.LittleEndian.AppendUint16(result, v)
	}
	return result
}

func newDNSRule(server netip.Addr, namespaces []string) (state.DNSRuleRecord, []dnsRegistryValue, error) {
	if !server.Is4() || !server.IsLoopback() || len(namespaces) == 0 {
		return state.DNSRuleRecord{}, nil, ErrConfiguration
	}
	var names []byte
	for _, name := range namespaces {
		if name == "" || len(name) > 254 {
			return state.DNSRuleRecord{}, nil, ErrConfiguration
		}
		for _, c := range name {
			valid := c == '.' || c == '-' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
			if !valid {
				return state.DNSRuleRecord{}, nil, ErrConfiguration
			}
		}
		names = append(names, utf16Bytes(name+"\x00")...)
	}
	names = append(names, 0, 0)
	if len(names) > 65536 {
		return state.DNSRuleRecord{}, nil, ErrConfiguration
	}
	values := []dnsRegistryValue{
		{"ConfigOptions", registry.DWORD, []byte{8, 0, 0, 0}},
		{"GenericDNSServers", registry.SZ, utf16Bytes(server.String() + "\x00")},
		{"Name", registry.MULTI_SZ, names},
		{"Version", registry.DWORD, []byte{2, 0, 0, 0}},
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return state.DNSRuleRecord{}, nil, err
	}
	return state.DNSRuleRecord{Key: id.String(), EffectStamp: state.EffectStamp{Phase: state.EffectPrepared, Fingerprint: dnsRuleFingerprint(values)}}, values, nil
}

func (db windowsDNSRegistry) keyPath(record state.DNSRuleRecord) (*uint16, error) {
	raw, decodeErr := hex.DecodeString(record.Fingerprint)
	if db.root == 0 || db.path == "" || decodeErr != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != record.Fingerprint {
		return nil, ErrConfiguration
	}
	if _, err := canonicalGUID(record.Key); err != nil {
		return nil, err
	}
	return windows.UTF16PtrFromString(db.path + `\{` + record.Key + "}")
}

func (db windowsDNSRegistry) install(ctx context.Context, record state.DNSRuleRecord, values []dnsRegistryValue) error {
	return registryTransaction(ctx, func(tx windows.Handle) error {
		return db.installTx(tx, record, values)
	})
}

func (db windowsDNSRegistry) installTx(tx windows.Handle, record state.DNSRuleRecord, values []dnsRegistryValue) error {
	path, err := db.keyPath(record)
	if err != nil {
		return err
	}
	if record.Phase != state.EffectPrepared || record.Fingerprint != dnsRuleFingerprint(values) {
		return ErrConfiguration
	}
	var k registry.Key
	var disposition uint32
	rc, _, _ := createRuleKey.Call(uintptr(db.root), uintptr(unsafe.Pointer(path)), 0, 0, 0, registry.SET_VALUE, 0, uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&disposition)), uintptr(tx), 0)
	if rc != 0 {
		return syscall.Errno(rc)
	}
	defer func() { _ = k.Close() }()
	if disposition != 1 {
		return ErrIPConflict
	}
	for _, value := range values {
		name, err := windows.UTF16PtrFromString(value.Name)
		if err != nil || len(value.Data) == 0 {
			return ErrConfiguration
		}
		rc, _, _ = setRuleValue.Call(uintptr(k), uintptr(unsafe.Pointer(name)), 0, uintptr(value.Kind), uintptr(unsafe.Pointer(&value.Data[0])), uintptr(len(value.Data)))
		if rc != 0 {
			return syscall.Errno(rc)
		}
	}
	return nil
}

func registryRuleFingerprint(k registry.Key) (string, error) {
	info, err := k.Stat()
	if err != nil {
		return "", err
	}
	if info.SubKeyCount != 0 || info.ValueCount != 4 || info.MaxValueLen > 65536 || info.MaxValueNameLen > 32 {
		return "", ErrIPConflict
	}
	names, err := k.ReadValueNames(4)
	if err != nil {
		return "", err
	}
	slices.Sort(names)
	values := make([]dnsRegistryValue, 0, len(names))
	for _, name := range names {
		data := make([]byte, info.MaxValueLen)
		n, kind, err := k.GetValue(name, data)
		if err != nil {
			return "", err
		}
		if n > len(data) {
			return "", ErrIPConflict
		}
		values = append(values, dnsRegistryValue{name, kind, data[:n]})
	}
	return dnsRuleFingerprint(values), nil
}

func (db windowsDNSRegistry) remove(ctx context.Context, record state.DNSRuleRecord) error {
	return registryTransaction(ctx, func(tx windows.Handle) error {
		return db.removeTx(tx, record)
	})
}

func (db windowsDNSRegistry) removeTx(tx windows.Handle, record state.DNSRuleRecord) error {
	path, err := db.keyPath(record)
	if err != nil {
		return err
	}
	if record.Phase != state.EffectRemoving {
		return ErrConfiguration
	}
	var k registry.Key
	rc, _, _ := openRuleKey.Call(uintptr(db.root), uintptr(unsafe.Pointer(path)), 0, registry.READ|windows.DELETE, uintptr(unsafe.Pointer(&k)), uintptr(tx), 0)
	if rc != 0 {
		err := syscall.Errno(rc)
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return nil
		}
		return err
	}
	defer func() { _ = k.Close() }()
	measured, err := registryRuleFingerprint(k)
	if err != nil {
		return err
	}
	if measured != record.Fingerprint {
		return ErrIPConflict
	}
	rc, _, _ = deleteRuleKey.Call(uintptr(db.root), uintptr(unsafe.Pointer(path)), 0, 0, uintptr(tx), 0)
	if rc != 0 {
		return syscall.Errno(rc)
	}
	return nil
}
