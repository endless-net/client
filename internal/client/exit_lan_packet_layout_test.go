package client

import (
	"encoding/binary"
	"testing"
)

func exitLANPacketBTFFixture(order binary.ByteOrder) *exitLANBTFTestBuilder {
	b := &exitLANBTFTestBuilder{order: order}
	u8 := b.add("u8", 1, 0, 1, 8)
	u16 := b.add("u16", 1, 0, 2, 16)
	u32 := b.add("u32", 1, 0, 4, 32)
	u64 := b.add("u64", 1, 0, 8, 64)
	kn := b.add("kernfs_node", 4, 1, 16, b.name("id"), u64, 64)
	knptr := b.add("", 2, 0, kn)
	kobj := b.add("kobject", 4, 1, 16, b.name("sd"), knptr, 64)
	dev := b.add("device", 4, 1, 24, b.name("kobj"), kobj, 64)
	netdev := b.add("net_device", 4, 2, 40, b.name("ifindex"), u32, 64, b.name("dev"), dev, 128)
	netptr := b.add("", 2, 0, netdev)
	state := b.add("nf_hook_state", 4, 2, 16, b.name("pf"), u8, 0, b.name("out"), netptr, 64)
	stateptr := b.add("", 2, 0, state)
	skb := b.add("sk_buff", 4, 2, 48, b.name("mark"), u32, 256, b.name("_skb_refdst"), u64, 320)
	skbptr := b.add("", 2, 0, skb)
	b.add("bpf_nf_ctx", 4, 2, 16, b.name("state"), stateptr, 0, b.name("skb"), skbptr, 64)
	dst := b.add("dst_entry", 4, 1, 16, b.name("dev"), netptr, 0)
	b.add("rtable", 4, 3, 24, b.name("dst"), dst, 0, b.name("rt_uses_gateway"), u8, 128, b.name("rt_type"), u16, 144)
	b.add("rt6_info", 4, 2, 32, b.name("dst"), dst, 0, b.name("rt6i_flags"), u32, 192)
	return b
}

func TestExitLANPacketBTFRequiresCompleteExactLayout(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		b := exitLANPacketBTFFixture(order)
		layout, err := exitLANBTFPacketLayout(b.raw())
		want := exitLANPacketLayout{ctxSKB: 8, skbMark: 32, skbDst: 40, stateOut: 8, deviceIndex: 8, deviceSD: 32, kernfsID: 8, ipv4Gateway: 16, ipv4Type: 18, ipv6Flags: 24}
		if err != nil || layout != want {
			t.Fatal("unexpected layout", layout, err)
		}
		b.add("net_device", 4, 0, 40)
		if _, err := exitLANBTFPacketLayout(b.raw()); err == nil {
			t.Fatal("ambiguous device identity accepted")
		}
	}
	if _, err := exitLANBTFPacketLayout(exitLANBTFTestFixture(binary.LittleEndian).raw()); err == nil {
		t.Fatal("deadline-only BTF accepted as packet enforcement")
	}
}
