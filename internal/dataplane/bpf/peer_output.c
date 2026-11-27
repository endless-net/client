#include <linux/bpf.h>

static long (*set_hash)(struct __sk_buff *, __u32) = (void *)BPF_FUNC_set_hash;
static long (*load_bytes)(struct __sk_buff *, __u32, void *, __u32) = (void *)BPF_FUNC_skb_load_bytes;
static long (*store_bytes)(struct __sk_buff *, __u32, const void *, __u32, __u64) = (void *)BPF_FUNC_skb_store_bytes;
static long (*replace_sum)(struct __sk_buff *, __u32, __u64, __u64, __u64) = (void *)BPF_FUNC_l3_csum_replace;

const volatile __u32 frame_tag;
const volatile __u32 source_port;
const volatile __u32 destination_port;

__attribute__((section("tcx/egress"), used))
int mark_injection(struct __sk_buff *skb)
{
	// WG preserves the inner flow hash across encryption. This interface
	// is private to a single candidate and only its injector sends IP.
	set_hash(skb, frame_tag);
	return -1;
}

__attribute__((section("tcx/egress"), used))
int classify_local(struct __sk_buff *skb)
{
	unsigned char h[28];
	const int ip = 14;
	if (load_bytes(skb, ip, h, sizeof(h)) || h[0] != 0x45 || h[9] != 17 ||
	    h[12] != 127 || h[13] || h[14] || h[15] != 1 ||
	    h[16] != 127 || h[17] || h[18] || h[19] != 1 ||
	    (h[20] << 8 | h[21]) != source_port ||
	    (h[22] << 8 | h[23]) != destination_port)
		return -1;
	// Only the private loopback IPv4 envelope carries the class. Neither
	// the inner IP nor the datagram forwarded by the public bind changes.
	unsigned char tos = skb->hash == frame_tag ? 0x28 : 0x30;
	if (skb->hash && skb->hash != frame_tag) return 2;
	if (replace_sum(skb, ip+10, __builtin_bswap16(0x4500 | h[1]), __builtin_bswap16(0x4500 | tos), 2) ||
	    store_bytes(skb, ip+1, &tos, 1, 0))
		return 2;
	return -1;
}
char peer_license[] __attribute__((section("license"), used)) = "GPL";
