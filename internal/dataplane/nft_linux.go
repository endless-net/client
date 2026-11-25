package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/endless-net/client/internal/state"
)

const linuxNFT = "/usr/sbin/nft"

type linuxNFTTable struct {
	Family, Name, Comment string
	Handle                uint64
}

func linuxNFTLookup(ctx context.Context, receipt state.LinuxNetworkReceipt) (uint64, error) {
	if !linuxObjectName.MatchString(receipt.Table) {
		return 0, ErrConfiguration
	}
	data, err := exec.CommandContext(ctx, linuxNFT, "-j", "-a", "list", "tables", "inet").Output()
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, ErrPolicyPermission
	}
	var list struct {
		Entries []struct {
			Table *linuxNFTTable
		} `json:"nftables"`
	}
	if err := json.Unmarshal(data, &list); err != nil || list.Entries == nil {
		return 0, ErrPolicyOwnership
	}
	for _, entry := range list.Entries {
		if table := entry.Table; table != nil && table.Family == "inet" && table.Name == receipt.Table {
			if table.Handle == 0 || table.Comment != "endlessnet:"+receipt.Owner {
				return 0, ErrPolicyOwnership
			}
			return table.Handle, nil
		}
	}
	return 0, nil
}

func linuxNFTApply(ctx context.Context, rules string) error {
	command := exec.CommandContext(ctx, linuxNFT, "-f", "-")
	command.Stdin = strings.NewReader(rules)
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrConfiguration
	}
	return nil
}

// CleanLinuxNetwork retires the recorded filter only after the nonpersistent
// link is absent. Delete by verified handle, not by a potentially reused name.
func CleanLinuxNetwork(ctx context.Context, ledger state.LinuxNetworkLedger) error {
	receipt, err := ledger.Read(ctx)
	if errors.Is(err, state.ErrMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	links, err := net.Interfaces()
	if err != nil {
		return ErrAdapterOwnership
	}
	for _, link := range links {
		if link.Name == receipt.Table {
			return ErrAdapterOwnership
		}
	}
	handle, err := linuxNFTLookup(ctx, receipt)
	if err != nil {
		return err
	}
	if handle != 0 {
		if err := linuxNFTApply(ctx, "delete table inet handle "+strconv.FormatUint(handle, 10)+"\n"); err != nil {
			return err
		}
		if handle, err := linuxNFTLookup(ctx, receipt); err != nil || handle != 0 {
			return ErrPolicyOwnership
		}
	}
	return ledger.Retire(ctx, receipt)
}
