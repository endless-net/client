// Linux generic XDP on the candidate's private WG interface. Only IPv4/UDP
// from its paired loopback socket can supply a size for decrypted input.
struct xdp_md { unsigned int data, data_end; };
static long (*adjust_head)(struct xdp_md *, int) = (void *)44;
static long (*adjust_tail)(struct xdp_md *, int) = (void *)65;
enum { XDP_DROP = 1, XDP_PASS = 2 };

const volatile unsigned int outer_offset;
const volatile unsigned int local_port;
const volatile unsigned int kernel_port;

static inline unsigned int be16(const unsigned char *p)
{
	return (unsigned int)p[0] << 8 | p[1];
}

static inline int loopback(const unsigned char *p)
{
	return p[0] == 127 && !p[1] && !p[2] && p[3] == 1;
}

__attribute__((section("xdp"), used))
int peer_wire_size(struct xdp_md *ctx)
{
	unsigned int offset = outer_offset;
	if (offset < 44 || offset > 65507 || adjust_head(ctx, -(int)offset))
		return XDP_DROP;
	unsigned char *p = (void *)(long)ctx->data;
	unsigned char *end = (void *)(long)ctx->data_end;
	if (p + 28 > end || p[0] != 0x45 || p[9] != 17 ||
	    !loopback(p + 12) || !loopback(p + 16) ||
	    be16(p + 20) != local_port || be16(p + 22) != kernel_port)
		return XDP_DROP;
	unsigned int udp = be16(p + 24);
	if (udp < 40 || udp + 20 != be16(p + 2))
		return XDP_DROP;
	if (adjust_head(ctx, offset) || adjust_tail(ctx, 4))
		return XDP_DROP;
	p = (void *)(long)ctx->data;
	end = (void *)(long)ctx->data_end;
	if (p + 20 > end || p[0] >> 4 != 4)
		return XDP_DROP;
	unsigned int n = be16(p + 2);
	if (n < 20 || n + 32 > udp - 8 || n > 65475 || p + n + 4 > end)
		return XDP_DROP;
	p[n] = 'E';
	p[n + 1] = 'N';
	p[n + 2] = (udp - 8) >> 8;
	p[n + 3] = udp - 8;
	return XDP_PASS;
}

char peer_license[] __attribute__((section("license"), used)) = "GPL";
