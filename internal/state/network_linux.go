package state

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"golang.org/x/sys/unix"
)

const linuxNetworkFile = "network-effects"

var linuxEffectsName = regexp.MustCompile(`^en[a-f0-9]{12}$`)

// LinuxNetworkReceipt describes the one native effects owner of this
// installation. It is written before creating an nftables table.
type LinuxNetworkReceipt struct {
	Table      string
	Owner      string
	ExitRule   bool   `json:"exit_rule,omitempty"`
	DNSIndex   int    `json:"dns_index,omitempty"`
	DNSOldPort uint16 `json:"dns_old_port,omitempty"`
	DNSNewPort uint16 `json:"dns_new_port,omitempty"`
}

type LinuxNetworkLedger struct{ backend LinuxBackend }

func NewLinuxNetworkLedger(directory string) LinuxNetworkLedger {
	return LinuxNetworkLedger{backend: LinuxBackend{directory: directory}}
}

func (r LinuxNetworkReceipt) valid() bool {
	if !linuxEffectsName.MatchString(r.Table) || !validID(r.Owner) {
		return false
	}
	if r.DNSIndex == 0 {
		return r.DNSOldPort == 0 && r.DNSNewPort == 0
	}
	return r.DNSIndex > 0 && r.DNSNewPort != 0
}

func (l LinuxNetworkLedger) Read(ctx context.Context) (LinuxNetworkReceipt, error) {
	var result LinuxNetworkReceipt
	err := l.backend.Exclusive(ctx, func(slot Slot) error {
		receipt, err := readLinuxNetworkReceipt(slot.(linuxSlot))
		result = receipt
		return err
	})
	return result, err
}

func readLinuxNetworkReceipt(slot linuxSlot) (LinuxNetworkReceipt, error) {
	var result LinuxNetworkReceipt
	raw, err := slot.read(linuxNetworkFile, 256)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil || !result.valid() {
		return LinuxNetworkReceipt{}, ErrCorrupt
	}
	return result, nil
}

func (l LinuxNetworkLedger) Prepare(ctx context.Context, receipt LinuxNetworkReceipt) error {
	if !receipt.valid() {
		return ErrCorrupt
	}
	return l.backend.Exclusive(ctx, func(slot Slot) error {
		root := slot.(linuxSlot)
		current, err := readLinuxNetworkReceipt(root)
		switch {
		case err == nil && current == receipt:
			return nil
		case err == nil:
			return ErrStale
		case !errors.Is(err, ErrMissing):
			return err
		}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		return root.replace(linuxNetworkFile, raw)
	})
}

// Replace records a pending change to the current owner's native effects.
func (l LinuxNetworkLedger) Replace(ctx context.Context, prior, next LinuxNetworkReceipt) error {
	if !prior.valid() || !next.valid() || prior.Table != next.Table || prior.Owner != next.Owner {
		return ErrCorrupt
	}
	return l.backend.Exclusive(ctx, func(slot Slot) error {
		root := slot.(linuxSlot)
		found, err := readLinuxNetworkReceipt(root)
		if err != nil {
			return err
		}
		if found == next {
			return nil
		}
		if found != prior {
			return ErrStale
		}
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		return root.replace(linuxNetworkFile, raw)
	})
}

func (l LinuxNetworkLedger) Retire(ctx context.Context, receipt LinuxNetworkReceipt) error {
	if !receipt.valid() {
		return ErrCorrupt
	}
	return l.backend.Exclusive(ctx, func(slot Slot) error {
		root := slot.(linuxSlot)
		current, err := readLinuxNetworkReceipt(root)
		if errors.Is(err, ErrMissing) {
			return nil
		}
		if err != nil {
			return err
		}
		if current != receipt {
			return ErrStale
		}
		if err := unix.Unlinkat(int(root.directory.Fd()), linuxNetworkFile, 0); err != nil {
			return ErrUncertain
		}
		if err := root.directory.Sync(); err != nil {
			return ErrUncertain
		}
		return nil
	})
}
