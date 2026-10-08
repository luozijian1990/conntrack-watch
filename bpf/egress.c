// SPDX-License-Identifier: GPL-2.0
// Self-contained UAPI declarations; no build-host kernel headers required.
typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned int u32;
typedef unsigned long long u64;
#define SEC(n) __attribute__((section(n), used))
#define INLINE static __attribute__((always_inline)) inline
#define UINT(n, v) int (*n)[v]
#define TYPE(n, v) v *n

static void *(*map_lookup)(void *, const void *) = (void *)1;
static long (*map_update)(void *, const void *, const void *, u64) = (void *)2;
static long (*map_delete)(void *, const void *) = (void *)3;
static u64 (*ktime_ns)(void) = (void *)5;
static u64 (*pid_tgid)(void) = (void *)14;
static long (*get_comm)(void *, u32) = (void *)16;
static u64 (*cgroup_id)(void) = (void *)80;
static long (*read_kernel)(void *, u32, const void *) = (void *)113;
static long (*ring_output)(void *, void *, u64, u64) = (void *)130;

// Only these kernel fields are accessed. CO-RE resolves their actual offsets.
struct ns_common { u32 inum; } __attribute__((preserve_access_index));
struct net { struct ns_common ns; } __attribute__((preserve_access_index));
typedef struct { struct net *net; } possible_net_t;
struct sock_common { possible_net_t skc_net; } __attribute__((preserve_access_index));
struct sock { struct sock_common __sk_common; } __attribute__((preserve_access_index));
#define READ(dst, src) read_kernel(&(dst), sizeof(dst), __builtin_preserve_access_index(&(src)))

// Standard tracepoint ABI, validated against tracefs /format by the loader.
struct state_ctx {
    u64 common;
    u64 skaddr;
    u32 oldstate, newstate;
    u16 sport, dport, family, protocol;
    u8 saddr[4], daddr[4], saddr_v6[16], daddr_v6[16];
};

// Wire ABI v1, little endian, 96 bytes. No kernel pointer leaves the kernel.
struct event {
    u64 started_ns, observed_ns, cgroup;
    u32 pid, netns;
    u16 sport, dport, family, kind; // 1 attempt, 2 established, 3 closed before established
    u8 src[16], dst[16];
    char comm[16];
    u32 version, pad;
};
_Static_assert(sizeof(struct event) == 96, "event ABI");

struct { UINT(type, 1); UINT(max_entries, 65536); TYPE(key, u64); TYPE(value, struct event); } pending SEC(".maps");
struct { UINT(type, 1); UINT(max_entries, 65535); TYPE(key, u16); TYPE(value, u8); } ports SEC(".maps");
struct { UINT(type, 2); UINT(max_entries, 1); TYPE(key, u32); TYPE(value, u32); } host_netns SEC(".maps");
struct { UINT(type, 27); UINT(max_entries, 4194304); } events SEC(".maps");
// Per-CPU health counters: ring loss, map full, CO-RE read failure.
struct { UINT(type, 6); UINT(max_entries, 3); TYPE(key, u32); TYPE(value, u64); } health SEC(".maps");

INLINE void count(u32 key) {
    u64 *v = map_lookup(&health, &key);
    if (v) __sync_fetch_and_add(v, 1);
}
INLINE void emit(struct event *e) {
    if (ring_output(&events, e, sizeof(*e), 0)) count(0);
}

SEC("tracepoint/sock/inet_sock_set_state")
int trace_state(struct state_ctx *ctx) {
    if (ctx->protocol != 6 || (ctx->family != 2 && ctx->family != 10)) return 0;
    u64 key = ctx->skaddr;
    if (ctx->newstate == 2 && ctx->oldstate == 7) { // CLOSE -> SYN_SENT: active open only
        u16 port = ctx->dport;
        if (!map_lookup(&ports, &port)) return 0;
        struct event e = {};
        struct sock *sk = (void *)ctx->skaddr;
        struct net *net = 0;
        if (READ(net, sk->__sk_common.skc_net.net) || !net || READ(e.netns, net->ns.inum) || !e.netns) {
            count(2);
            return 0;
        }
        u32 zero = 0;
        u32 *host = map_lookup(&host_netns, &zero);
        if (!host || *host == e.netns) return 0;
        e.started_ns = e.observed_ns = ktime_ns();
        e.cgroup = cgroup_id();
        e.pid = pid_tgid() >> 32;
        get_comm(e.comm, sizeof(e.comm));
        e.sport = ctx->sport;
        e.dport = ctx->dport;
        e.family = ctx->family;
        e.kind = 1;
        e.version = 1;
        if (e.family == 2) {
            __builtin_memcpy(e.src, ctx->saddr, 4);
            __builtin_memcpy(e.dst, ctx->daddr, 4);
        } else {
            __builtin_memcpy(e.src, ctx->saddr_v6, 16);
            __builtin_memcpy(e.dst, ctx->daddr_v6, 16);
        }
        // Never evict silently: a full map is exposed through health metrics.
        if (map_update(&pending, &key, &e, 0)) count(1);
        emit(&e);
        return 0;
    }
    // Simultaneous open may pass through SYN_RECV. Keep identity until outcome.
    if (ctx->newstate != 1 && ctx->newstate != 7) return 0;
    struct event *saved = map_lookup(&pending, &key);
    if (!saved) return 0; // inbound sockets and sockets predating attachment are ignored
    struct event e = *saved;
    e.observed_ns = ktime_ns();
    if (ctx->sport) e.sport = ctx->sport;
    e.kind = ctx->newstate == 1 ? 2 : 3;
    map_delete(&pending, &key);
    emit(&e);
    return 0;
}
char LICENSE[] SEC("license") = "GPL";
