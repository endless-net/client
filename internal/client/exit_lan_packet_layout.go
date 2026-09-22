package client

import "math"

// Every offset is resolved from the running kernel's BTF. No compiled-in
// structure layout or guessed fallback may authorize a packet.
type exitLANPacketLayout struct {
	ctxSKB, ctxState, skbMark, skbDst           uint32
	stateOut, stateFamily                       uint32
	deviceIndex, deviceSD, kernfsID             uint32
	dstDevice, ipv4Gateway, ipv4Type, ipv6Flags uint32
}

func exitLANBTFPacketLayout(raw []byte) (exitLANPacketLayout, error) {
	var layout exitLANPacketLayout
	b, err := parseExitLANBTF(raw)
	if err != nil {
		return layout, err
	}
	b.pointerBytes = 8
	find := func(name string) (uint32, error) {
		var found uint32
		for id, typ := range b.types {
			if typ.name == name && typ.kind == 4 {
				if found != 0 {
					return 0, errExitLANBTF
				}
				found = uint32(id)
			}
		}
		if found == 0 {
			return 0, errExitLANBTF
		}
		return found, nil
	}
	type field struct {
		structure, name string
		width           uint64
		pointer         string
		target          *uint32
	}
	fields := []field{
		{"bpf_nf_ctx", "skb", 8, "sk_buff", &layout.ctxSKB},
		{"bpf_nf_ctx", "state", 8, "nf_hook_state", &layout.ctxState},
		{"sk_buff", "mark", 4, "", &layout.skbMark},
		{"sk_buff", "_skb_refdst", 8, "", &layout.skbDst},
		{"nf_hook_state", "out", 8, "net_device", &layout.stateOut},
		{"nf_hook_state", "pf", 1, "", &layout.stateFamily},
		{"net_device", "ifindex", 4, "", &layout.deviceIndex},
		{"kernfs_node", "id", 8, "", &layout.kernfsID},
		{"dst_entry", "dev", 8, "net_device", &layout.dstDevice},
		{"rtable", "rt_uses_gateway", 1, "", &layout.ipv4Gateway},
		{"rtable", "rt_type", 2, "", &layout.ipv4Type},
		{"rt6_info", "rt6i_flags", 4, "", &layout.ipv6Flags},
	}
	for _, field := range fields {
		id, err := find(field.structure)
		if err != nil {
			return layout, err
		}
		member, offset, err := b.member(id, field.name, map[uint32]bool{}, 0)
		if err != nil || member == 0 || offset > math.MaxInt16 {
			return layout, errExitLANBTF
		}
		resolved, err := b.resolve(member)
		if err != nil {
			return layout, err
		}
		size, err := b.sizeOf(resolved, 0)
		if err != nil || size != field.width {
			return layout, errExitLANBTF
		}
		if field.pointer != "" {
			target, err := find(field.pointer)
			if err != nil || b.types[resolved].kind != 2 {
				return layout, errExitLANBTF
			}
			actual, err := b.resolve(b.types[resolved].size)
			if err != nil || actual != target {
				return layout, errExitLANBTF
			}
		} else if b.types[resolved].kind != 1 {
			return layout, errExitLANBTF
		} else {
			encoding := b.order.Uint32(b.types[resolved].data)
			kind := encoding >> 24
			allowed := kind == 0 || field.name == "ifindex" && kind == 1 || field.name == "rt_uses_gateway" && kind == 4
			if encoding&0x00ffffff != uint32(field.width*8) || !allowed {
				return layout, errExitLANBTF
			}
		}
		*field.target = offset
	}
	// Both route structures embed dst at offset zero; scalar _skb_refdst is
	// read with probe_read_kernel, never cast into verifier-trusted memory.
	for _, name := range []string{"rtable", "rt6_info"} {
		id, err := find(name)
		if err != nil {
			return layout, err
		}
		member, offset, err := b.member(id, "dst", map[uint32]bool{}, 0)
		if err != nil || offset != 0 {
			return layout, errExitLANBTF
		}
		member, err = b.resolve(member)
		if err != nil || b.types[member].name != "dst_entry" || b.types[member].kind != 4 {
			return layout, errExitLANBTF
		}
	}
	// net_device.dev.kobj.sd identifies the exact registered sysfs object.
	id, err := find("net_device")
	if err != nil {
		return layout, err
	}
	var total uint32
	for _, field := range []struct{ name, typ string }{{"dev", "device"}, {"kobj", "kobject"}, {"sd", "kernfs_node"}} {
		member, offset, err := b.member(id, field.name, map[uint32]bool{}, 0)
		if err != nil || member == 0 {
			return layout, errExitLANBTF
		}
		total += offset
		id, err = b.resolve(member)
		if err != nil {
			return layout, err
		}
		if field.name == "sd" {
			if b.types[id].kind != 2 {
				return layout, errExitLANBTF
			}
			id, err = b.resolve(b.types[id].size)
			if err != nil {
				return layout, err
			}
		}
		if b.types[id].kind != 4 || b.types[id].name != field.typ || total > math.MaxInt16 {
			return layout, errExitLANBTF
		}
	}
	layout.deviceSD = total
	return layout, nil
}
