package dataplane

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/state"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
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
	handle, err := linuxNFTLookup(ctx, receipt)
	if err != nil {
		return err
	}
	links, err := remainingLinuxPeers(ctx, receipt)
	if err != nil {
		return err
	}
	for _, index := range links {
		if err := deleteLinuxPeerIndex(ctx, index); err != nil {
			return err
		}
	}
	if len(links) > 0 {
		remaining, err := remainingLinuxPeers(ctx, receipt)
		if err != nil {
			return err
		}
		if len(remaining) != 0 {
			return ErrAdapterOwnership
		}
	}
	if err := waitResolvedRemoval(ctx, receipt); err != nil {
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

type linuxPeerLinkInfo struct {
	Name  string   `json:"ifname"`
	Index int      `json:"ifindex"`
	Group string   `json:"group"`
	Alias string   `json:"ifalias"`
	Flags []string `json:"flags"`
	Info  struct {
		Kind string `json:"info_kind"`
	} `json:"linkinfo"`
}

func linuxLinkDetails(ctx context.Context, name ...string) ([]linuxPeerLinkInfo, error) {
	args := []string{"-N", "-j", "-d", "link", "show"}
	data, err := exec.CommandContext(ctx, linuxIP, append(args, name...)...).Output()
	if err != nil {
		return nil, ErrAdapterOwnership
	}
	var links []linuxPeerLinkInfo
	if err := json.Unmarshal(data, &links); err != nil || len(links) == 0 {
		return nil, ErrAdapterOwnership
	}
	return links, nil
}

// Read all links in this network namespace. The owner group is installed in
// the exclusive creation request, before the device can receive a private key.
func remainingLinuxPeers(ctx context.Context, receipt state.LinuxNetworkReceipt) ([]int, error) {
	links, err := linuxLinkDetails(ctx)
	if err != nil {
		return nil, err
	}
	prefix, group := linuxPeerScope(receipt.Owner)
	groupID := strconv.FormatUint(uint64(group), 10)
	var owned []int
	for _, link := range links {
		if link.Name == receipt.Table || link.Index <= 0 || link.Name == "" {
			return nil, ErrAdapterOwnership
		}
		ownedAlias := link.Alias == "endlessnet:peer:"+receipt.Owner
		if link.Group != groupID || !strings.HasPrefix(link.Name, prefix) {
			if ownedAlias {
				return nil, ErrAdapterOwnership
			}
			continue
		}
		if !ownedAlias && (link.Alias != "" || slices.Contains(link.Flags, "UP")) {
			return nil, ErrAdapterOwnership
		}
		if !linuxPeerLinkName.MatchString(link.Name) || (link.Info.Kind != transport.WireGuard && link.Info.Kind != transport.AmneziaWG) {
			return nil, ErrAdapterOwnership
		}
		owned = append(owned, link.Index)
	}
	return owned, nil
}

// Address the identified interface by its kernel index so reuse of its name
// between inspection and deletion does not target the replacement.
func deleteLinuxPeerIndex(ctx context.Context, index int) error {
	conn, err := netlink.Dial(unix.NETLINK_ROUTE, nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	data := make([]byte, unix.SizeofIfInfomsg)
	binary.NativeEndian.PutUint32(data[4:8], uint32(index))
	_, err = conn.Execute(netlink.Message{Header: netlink.Header{Type: unix.RTM_DELLINK, Flags: netlink.Request | netlink.Acknowledge}, Data: data})
	if errors.Is(err, unix.ENODEV) {
		return nil
	}
	return err
}
