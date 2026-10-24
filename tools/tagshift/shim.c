// shim.c — #3128 write-channel tracer for VMRunner (v4).
//
// Public-linkable symbols only (linked normally, so __interpose tuples
// hold real target addresses). Wrappers are ts_* and forward via real_*
// dlsym pointers or a raw svc fallback for the early-init window.
//
// Coverage: open/openat, write/pwrite/writev/pwritev, aio_write,
// fsync/fdatasync, fcntl/ioctl, syscall, guarded_*_np, dispatch_io_*
// (incl. _f), IOKit IOServiceOpen + IOConnectCall*/MapMemory, and
// mach_msg (rate-limited — IOConnectCall* bottom out in mach_msg, so a
// silent IOConnectCall* + chatty mach_msg on one port tells the same
// story without logging every message).
//
// Writes on the device fd log offset+len+32B prefix and append a
// {magic,off,len,payload} record to <log>.data.
// Env: TSLOG (path), TSDEV (device path — rdisk fd found via st_rdev).

#define _DARWIN_C_SOURCE
#include <fcntl.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/time.h>
#include <sys/uio.h>
#include <pthread.h>
#include <unistd.h>
#include <dispatch/dispatch.h>
#include <dispatch/io.h>
#include <IOKit/IOKitLib.h>
#include <mach/mach.h>
#include <dlfcn.h>
#include <aio.h>

// Raw BSD syscall: bypasses symbol resolution during early init/reentry.
static inline long raw_sys6(long n, long a0, long a1, long a2, long a3, long a4, long a5) {
    register long x0 __asm__("x0") = a0, x1 __asm__("x1") = a1, x2 __asm__("x2") = a2,
                  x3 __asm__("x3") = a3, x4 __asm__("x4") = a4, x5 __asm__("x5") = a5;
    register long x16 __asm__("x16") = n;
    __asm__ volatile("svc #0x80"
                     : "+r"(x0)
                     : "r"(x1), "r"(x2), "r"(x3), "r"(x4), "r"(x5), "r"(x16)
                     : "memory", "cc");
    return x0;
}
static void ensure_bound(void);

extern int fdatasync(int);
extern dispatch_io_t dispatch_io_create_f(dispatch_io_type_t, dispatch_fd_t, dispatch_queue_t, void (^)(dispatch_fd_t, int));
extern int guarded_pwrite_np(int, const void *, size_t, off_t);
extern int guarded_write_np(int, const void *, size_t);
#define R(f) static __typeof(&f) real_##f
R(open); R(openat);
R(write); R(pwrite); R(writev); R(pwritev); R(aio_write);
R(fsync); R(fdatasync); R(fcntl); R(ioctl); R(syscall);
R(guarded_pwrite_np); R(guarded_write_np);
R(dispatch_io_create); R(dispatch_io_create_with_path); R(dispatch_io_create_f);
R(dispatch_io_write); R(dispatch_io_read); R(dispatch_io_close);
R(IOServiceOpen);
static io_service_t (*real_IOServiceGetMatchingService)(mach_port_t, CFDictionaryRef);
R(IOConnectCallMethod); R(IOConnectCallAsyncMethod);
R(IOConnectCallStructMethod); R(IOConnectCallScalarMethod);
R(IOConnectMapMemory); R(IOConnectMapMemory64);

static int g_logfd = -1;
static int g_datafd = -1;
static dev_t g_rdev = 0;
static char g_dev[128] = {0};
static volatile int g_bound = 0;
static pthread_mutex_t g_mu = PTHREAD_MUTEX_INITIALIZER;

// logf_ must never touch libc-with-once (snprintf pulls locale once-init —
// called reentrantly during XPC bundle resolution it SIGTRAPs). Formats
// %s %d %lld %u %llu %x %llx %p %zu manually, writes via raw_sys6.
static void lw_(const char *s, size_t n) {
    if (g_logfd < 0) return;
    raw_sys6(SYS_write, g_logfd, (long)s, n, 0, 0, 0);
}
static void lws_(const char *s) { lw_(s, strlen(s)); }
static void lnum_(unsigned long long v, int neg) {
    char b[21]; int i = 20;
    do { b[--i] = '0' + (v % 10); v /= 10; } while (v && i);
    if (neg) b[--i] = '-';
    lw_(b + i, 20 - i);
}
static void lhex_(unsigned long long v) {
    char b[17]; const char *d = "0123456789abcdef"; int i = 16;
    do { b[--i] = d[v & 15]; v >>= 4; } while (v && i);
    lw_(b + i, 16 - i);
}
static void logf_(const char *fmt, ...) {
    if (g_logfd < 0) return;
    va_list ap; va_start(ap, fmt);
    struct timeval tv; gettimeofday(&tv, NULL);
    pthread_mutex_lock(&g_mu);
    lws_("[");
    lnum_((unsigned long long)tv.tv_sec, 0);
    lws_(".");
    lnum_((unsigned long long)tv.tv_usec, 0);
    lws_(" t=");
    lhex_((unsigned long long)pthread_self());
    lws_("] ");
    for (const char *f = fmt; *f; ) {
        const char *p = f;
        while (*p && *p != '%') p++;
        if (p != f) lw_(f, p - f);
        if (!*p) break;
        f = ++p;
        int lng = 0;
        while (*f == 'l') { lng++; f++; }
        switch (*f) {
        case 's': { const char *v = va_arg(ap, const char *); lws_(v ? v : "(null)"); f++; break; }
        case 'd': case 'i': {
            if (lng) { long long v = va_arg(ap, long long); lnum_(v < 0 ? -v : v, v < 0); }
            else { int v = va_arg(ap, int); lnum_(v < 0 ? -(long long)v : v, v < 0); }
            f++; break; }
        case 'u': {
            if (lng) lnum_(va_arg(ap, unsigned long long), 0);
            else lnum_(va_arg(ap, unsigned int), 0);
            f++; break; }
        case 'x': case 'X': {
            if (lng) lhex_(va_arg(ap, unsigned long long));
            else lhex_(va_arg(ap, unsigned int));
            f++; break; }
        case 'p': lhex_((unsigned long long)va_arg(ap, void *)); f++; break;
        case 'z': if (*++f == 'u') { lnum_(va_arg(ap, size_t), 0); f++; } break;
        case '%': lw_("%", 1); f++; break;
        default: lw_("%", 1); if (*f) { lw_(f, 1); f++; } break;
        }
    }
    pthread_mutex_unlock(&g_mu);
    va_end(ap);
}

static int is_dev_fd(int fd) {
    if (fd < 0 || !g_rdev) return 0;
    struct stat st;
    return fstat(fd, &st) == 0 && S_ISCHR(st.st_mode) && st.st_rdev == g_rdev;
}

static void hexs(char *buf, const void *p, int n) {
    static const char d[] = "0123456789abcdef";
    const uint8_t *b = p;
    for (int i = 0; i < n; i++) { buf[2*i] = d[b[i] >> 4]; buf[2*i+1] = d[b[i] & 15]; }
    buf[2 * n] = 0;
}

static void dump_data(int64_t off, const void *p, size_t len) {
    if (g_datafd < 0 || !p || !len) return;
    struct { uint32_t magic; int64_t off; uint32_t len; } h = {0x54534441, off, (uint32_t)len};
    pthread_mutex_lock(&g_mu);
    if (real_write) { real_write(g_datafd, &h, sizeof h); real_write(g_datafd, p, len); }
    else { raw_sys6(SYS_write, g_datafd, (long)&h, sizeof h, 0, 0, 0); raw_sys6(SYS_write, g_datafd, (long)p, len, 0, 0, 0); }
    pthread_mutex_unlock(&g_mu);
}

// ---- open family -----------------------------------------------------------
static void note_open(int fd, const char *path, const char *sym) {
    if (fd < 0 || !path) return;
    if (strstr(path, "rdisk") || strstr(path, "/dev/disk") || strstr(path, "/dev/null")) {
        struct stat st;
        int hit = (fstat(fd, &st) == 0 && g_rdev && st.st_rdev == g_rdev);
        logf_("%s(%s) -> %d%s\n", sym, path, fd, hit ? " [TSDEV]" : "");
    }
}
int ts_open(const char *path, int flags, ...) {
    ensure_bound();
    va_list ap; va_start(ap, flags); int mode = va_arg(ap, int); va_end(ap);
    int fd = real_open ? real_open(path, flags, mode)
                       : (int)raw_sys6(SYS_open, (long)path, flags, mode, 0, 0, 0);
    note_open(fd, path, "open");
    return fd;
}
int ts_openat(int dfd, const char *path, int flags, ...) {
    ensure_bound();
    va_list ap; va_start(ap, flags); int mode = va_arg(ap, int); va_end(ap);
    int fd = real_openat ? real_openat(dfd, path, flags, mode)
                         : (int)raw_sys6(SYS_openat, dfd, (long)path, flags, mode, 0, 0);
    note_open(fd, path, "openat");
    return fd;
}

// ---- write-ish -------------------------------------------------------------
ssize_t ts_write(int fd, const void *buf, size_t n) {
    ensure_bound();
    if (is_dev_fd(fd)) {
        char h[65]; hexs(h, buf, n < 32 ? (int)n : 32);
        logf_("write(fd=%d,len=%zu) %s%s\n", fd, n, h, n > 32 ? "…" : "");
        dump_data(-1, buf, n);
    }
    return real_write ? real_write(fd, buf, n) : raw_sys6(SYS_write, fd, (long)buf, n, 0, 0, 0);
}
ssize_t ts_pwrite(int fd, const void *buf, size_t n, off_t off) {
    ensure_bound();
    if (is_dev_fd(fd)) {
        char h[65]; hexs(h, buf, n < 32 ? (int)n : 32);
        logf_("pwrite(fd=%d,off=%lld,len=%zu) %s%s\n", fd, (long long)off, n, h, n > 32 ? "…" : "");
        dump_data(off, buf, n);
    }
    return real_pwrite ? real_pwrite(fd, buf, n, off) : raw_sys6(SYS_pwrite, fd, (long)buf, n, off, 0, 0);
}
ssize_t ts_writev(int fd, const struct iovec *iov, int cnt) {
    ensure_bound();
    if (is_dev_fd(fd)) {
        size_t tot = 0; for (int i = 0; i < cnt; i++) tot += iov[i].iov_len;
        char h[65]; hexs(h, iov[0].iov_base, iov[0].iov_len < 32 ? (int)iov[0].iov_len : 32);
        logf_("writev(fd=%d,iov=%d,tot=%zu) %s\n", fd, cnt, tot, h);
    }
    return real_writev ? real_writev(fd, iov, cnt) : raw_sys6(SYS_writev, fd, (long)iov, cnt, 0, 0, 0);
}
ssize_t ts_pwritev(int fd, const struct iovec *iov, int cnt, off_t off) {
    ensure_bound();
    if (is_dev_fd(fd)) {
        size_t tot = 0; for (int i = 0; i < cnt; i++) tot += iov[i].iov_len;
        logf_("pwritev(fd=%d,off=%lld,iov=%d,tot=%zu)\n", fd, (long long)off, cnt, tot);
        for (int i = 0; i < cnt; i++) { dump_data(off, iov[i].iov_base, iov[i].iov_len); off += iov[i].iov_len; }
    }
    return real_pwritev ? real_pwritev(fd, iov, cnt, off) : raw_sys6(SYS_pwritev, fd, (long)iov, cnt, off, 0, 0);
}
int ts_aio_write(struct aiocb *a) {
    ensure_bound();
    if (a && is_dev_fd(a->aio_fildes))
        logf_("aio_write(fd=%d,off=%lld,len=%zu)\n", a->aio_fildes,
              (long long)a->aio_offset, a->aio_nbytes);
    return real_aio_write ? real_aio_write(a) : (int)raw_sys6(SYS_aio_write, (long)a, 0, 0, 0, 0, 0);
}
int ts_fsync(int fd) {
    ensure_bound();
    if (is_dev_fd(fd)) logf_("fsync(fd=%d)\n", fd);
    return real_fsync ? real_fsync(fd) : (int)raw_sys6(SYS_fsync, fd, 0, 0, 0, 0, 0);
}
int ts_fdatasync(int fd) {
    ensure_bound();
    if (is_dev_fd(fd)) logf_("fdatasync(fd=%d)\n", fd);
    return real_fdatasync ? real_fdatasync(fd) : (int)raw_sys6(SYS_fdatasync, fd, 0, 0, 0, 0, 0);
}
int ts_fcntl(int fd, int cmd, ...) {
    ensure_bound();
    va_list ap; va_start(ap, cmd); long a = va_arg(ap, long); va_end(ap);
    if (is_dev_fd(fd)) logf_("fcntl(fd=%d,cmd=%d,arg=%ld)\n", fd, cmd, a);
    return real_fcntl ? real_fcntl(fd, cmd, a) : (int)raw_sys6(SYS_fcntl, fd, cmd, a, 0, 0, 0);
}
int ts_ioctl(int fd, unsigned long req, ...) {
    ensure_bound();
    va_list ap; va_start(ap, req); void *a = va_arg(ap, void *); va_end(ap);
    if (is_dev_fd(fd)) logf_("ioctl(fd=%d,req=0x%lx,arg=%p)\n", fd, req, a);
    return real_ioctl ? real_ioctl(fd, req, a) : (int)raw_sys6(SYS_ioctl, fd, req, (long)a, 0, 0, 0);
}
int ts_guarded_pwrite_np(int fd, const void *buf, size_t n, off_t off) {
    ensure_bound();
    if (is_dev_fd(fd)) logf_("guarded_pwrite_np(fd=%d,off=%lld,len=%zu)\n", fd, (long long)off, n);
    return real_guarded_pwrite_np ? real_guarded_pwrite_np(fd, buf, n, off) : -1;
}
int ts_guarded_write_np(int fd, const void *buf, size_t n) {
    ensure_bound();
    if (is_dev_fd(fd)) logf_("guarded_write_np(fd=%d,len=%zu)\n", fd, n);
    return real_guarded_write_np ? real_guarded_write_np(fd, buf, n) : -1;
}
int ts_syscall(int num, ...) {
    ensure_bound();
    va_list ap; va_start(ap, num);
    long a1 = va_arg(ap, long), a2 = va_arg(ap, long), a3 = va_arg(ap, long),
         a4 = va_arg(ap, long), a5 = va_arg(ap, long), a6 = va_arg(ap, long);
    va_end(ap);
    switch (num) {
    case SYS_write: case SYS_write_nocancel: case SYS_pwrite:
    case SYS_pwrite_nocancel: case SYS_writev: case SYS_pwritev:
    case SYS_writev_nocancel: case SYS_pwritev_nocancel:
    case SYS_aio_write: case SYS_fsync: case SYS_fsync_nocancel:
    case SYS_ioctl: case SYS_read: case SYS_pread:
    case SYS_pread_nocancel: case SYS_read_nocancel:
    case SYS_readv: case SYS_preadv:
        if (is_dev_fd((int)a1))
            logf_("syscall(num=%d fd=%ld a2=%lx a3=%lx a4=%lx)\n", num, a1, a2, a3, a4);
    }
    return real_syscall ? real_syscall(num, a1, a2, a3, a4, a5, a6)
                        : (int)raw_sys6(num, a1, a2, a3, a4, a5, a6);
}

// ---- dispatch_io -----------------------------------------------------------
#define MAXCH 64
static dispatch_io_t g_ch[MAXCH];
static int g_nch = 0;
static int is_dev_ch(dispatch_io_t ch) {
    for (int i = 0; i < g_nch; i++) if (g_ch[i] == ch) return 1;
    return 0;
}
static void note_ch(dispatch_io_t ch) { if (ch && g_nch < MAXCH) g_ch[g_nch++] = ch; }

dispatch_io_t ts_dio_create(dispatch_io_type_t t, dispatch_fd_t fd, dispatch_queue_t q, void (^h)(int)) {
    ensure_bound();
    dispatch_io_t ch = real_dispatch_io_create(t, fd, q, h);
    if (is_dev_fd(fd)) { note_ch(ch); logf_("dispatch_io_create(type=%d,fd=%d) -> %p\n", (int)t, fd, ch); }
    return ch;
}
dispatch_io_t ts_dio_create_path(dispatch_io_type_t t, const char *path, int oflag, mode_t mode, dispatch_queue_t q, void (^h)(int)) {
    ensure_bound();
    dispatch_io_t ch = real_dispatch_io_create_with_path(t, path, oflag, mode, q, h);
    if (path && (strstr(path, "rdisk") || (g_dev[0] && strstr(path, g_dev)))) {
        note_ch(ch);
        logf_("dispatch_io_create_with_path(type=%d,path=%s) -> %p\n", (int)t, path, ch);
    }
    return ch;
}
dispatch_io_t ts_dio_create_f(dispatch_io_type_t t, dispatch_fd_t fd, dispatch_queue_t q, void (^h)(dispatch_fd_t, int)) {
    ensure_bound();
    dispatch_io_t ch = real_dispatch_io_create_f(t, fd, q, h);
    if (is_dev_fd(fd)) { note_ch(ch); logf_("dispatch_io_create_f(type=%d,fd=%d) -> %p\n", (int)t, fd, ch); }
    return ch;
}
void ts_dio_write(dispatch_io_t ch, off_t off, dispatch_data_t d, dispatch_queue_t q, void (^h)(bool, dispatch_data_t, int)) {
    ensure_bound();
    if (is_dev_ch(ch)) {
        size_t sz = d ? dispatch_data_get_size(d) : 0;
        logf_("dispatch_io_write(ch=%p,off=%lld,len=%zu)\n", ch, (long long)off, sz);
        if (d && g_datafd >= 0) {
            const void *ptr = NULL; size_t psz = 0;
            dispatch_data_t m = dispatch_data_create_map(d, &ptr, &psz);
            dump_data(off, ptr, psz);
            if (m) dispatch_release(m);
        }
    }
    real_dispatch_io_write(ch, off, d, q, h);
}
void ts_dio_read(dispatch_io_t ch, off_t off, size_t len, dispatch_queue_t q, void (^h)(bool, dispatch_data_t, int)) {
    ensure_bound();
    if (is_dev_ch(ch)) logf_("dispatch_io_read(ch=%p,off=%lld,len=%zu)\n", ch, (long long)off, len);
    real_dispatch_io_read(ch, off, len, q, h);
}
void ts_dio_close(dispatch_io_t ch, dispatch_io_close_flags_t f) {
    ensure_bound();
    if (is_dev_ch(ch)) logf_("dispatch_io_close(ch=%p)\n", ch);
    real_dispatch_io_close(ch, f);
}

// ---- IOKit -----------------------------------------------------------------
kern_return_t ts_IOServiceOpen(io_service_t svc, task_port_t task, uint32_t type, io_connect_t *conn) {
    ensure_bound();
    kern_return_t r = real_IOServiceOpen(svc, task, type, conn);
    io_name_t nm = {0};
    IOObjectGetClass(svc, nm);
    logf_("IOServiceOpen(%s,type=%u) -> %d conn=%u\n", nm, type, r, conn ? *conn : 0);
    return r;
}
io_service_t ts_IOServiceGetMatchingService(mach_port_t mp, CFDictionaryRef matching) {
    ensure_bound();
    io_service_t svc = real_IOServiceGetMatchingService(mp, matching);
    CFStringRef nm = matching ? (CFStringRef)CFDictionaryGetValue(matching, CFSTR(kIOProviderClassKey)) : NULL;
    char nb[128] = {0};
    if (nm) CFStringGetCString(nm, nb, sizeof nb, kCFStringEncodingUTF8);
    logf_("IOServiceGetMatchingService(class=%s) -> svc=%u\n", nb, svc);
    return svc;
}
kern_return_t ts_IOConnectCallMethod(mach_port_t c, uint32_t sel, const uint64_t *in, uint32_t inc, const void *sIn, size_t sInSz, uint64_t *out, uint32_t *outc, void *sOut, size_t *sOutSz) {
    ensure_bound();
    kern_return_t r = real_IOConnectCallMethod(c, sel, in, inc, sIn, sInSz, out, outc, sOut, sOutSz);
    char h[65] = {0}; if (sIn && sInSz) hexs(h, sIn, sInSz < 32 ? (int)sInSz : 32);
    logf_("IOConnectCallMethod(conn=%u,sel=%u,scalIn=%u,structIn=%zu) %s\n", c, sel, inc, sInSz, h);
    return r;
}
kern_return_t ts_IOConnectCallAsyncMethod(mach_port_t c, uint32_t sel, mach_port_t wp, uint64_t *refs, uint32_t rc, const uint64_t *in, uint32_t inc, const void *sIn, size_t sInSz, uint64_t *out, uint32_t *outc, void *sOut, size_t *sOutSz) {
    ensure_bound();
    kern_return_t r = real_IOConnectCallAsyncMethod(c, sel, wp, refs, rc, in, inc, sIn, sInSz, out, outc, sOut, sOutSz);
    char h[65] = {0}; if (sIn && sInSz) hexs(h, sIn, sInSz < 32 ? (int)sInSz : 32);
    logf_("IOConnectCallAsyncMethod(conn=%u,sel=%u,scalIn=%u,structIn=%zu) %s\n", c, sel, inc, sInSz, h);
    return r;
}
kern_return_t ts_IOConnectCallStructMethod(mach_port_t c, uint32_t sel, const void *sIn, size_t sInSz, void *sOut, size_t *sOutSz) {
    ensure_bound();
    kern_return_t r = real_IOConnectCallStructMethod(c, sel, sIn, sInSz, sOut, sOutSz);
    char h[65] = {0}; if (sIn && sInSz) hexs(h, sIn, sInSz < 32 ? (int)sInSz : 32);
    logf_("IOConnectCallStructMethod(conn=%u,sel=%u,structIn=%zu) %s\n", c, sel, sInSz, h);
    return r;
}
kern_return_t ts_IOConnectCallScalarMethod(mach_port_t c, uint32_t sel, const uint64_t *in, uint32_t inc, uint64_t *out, uint32_t *outc) {
    ensure_bound();
    kern_return_t r = real_IOConnectCallScalarMethod(c, sel, in, inc, out, outc);
    char h[129] = {0}; size_t hn = 0;
    static const char hd[] = "0123456789abcdef";
    for (uint32_t i = 0; i < inc && i < 8 && hn + 17 < sizeof h; i++) {
        uint64_t v = in[i]; int k = 16; char tmp[17]; tmp[16] = 0;
        do { tmp[--k] = hd[v & 15]; v >>= 4; } while (v && k);
        memcpy(h + hn, tmp + k, 16 - k); hn += 16 - k; h[hn++] = ' '; }
    h[hn] = 0;
    logf_("IOConnectCallScalarMethod(conn=%u,sel=%u,in=[%s])\n", c, sel, h);
    return r;
}
kern_return_t ts_IOConnectMapMemory(io_connect_t c, uint32_t t, task_port_t task, mach_vm_address_t *addr, mach_vm_size_t *size, IOOptionBits opts) {
    ensure_bound();
    kern_return_t r = real_IOConnectMapMemory(c, t, task, addr, size, opts);
    logf_("IOConnectMapMemory(conn=%u,type=%u) -> %d addr=%p size=%llu\n",
          c, t, r, addr ? (void *)*addr : 0, size ? *size : 0);
    return r;
}
kern_return_t ts_IOConnectMapMemory64(io_connect_t c, uint32_t t, task_port_t task, mach_vm_address_t *addr, mach_vm_size_t *size, IOOptionBits opts) {
    ensure_bound();
    kern_return_t r = real_IOConnectMapMemory64(c, t, task, addr, size, opts);
    logf_("IOConnectMapMemory64(conn=%u,type=%u) -> %d addr=%p size=%llu\n",
          c, t, r, addr ? (void *)*addr : 0, size ? *size : 0);
    return r;
}


// ---- binding ---------------------------------------------------------------
#define RN(f) do { real_##f = dlsym(RTLD_NEXT, #f); \
    if (!real_##f) real_##f = dlsym(RTLD_DEFAULT, #f); } while (0)

static void ensure_bound(void) {
    if (g_bound) return;
    g_bound = 1;
    RN(open); RN(openat);
    RN(write); RN(pwrite); RN(writev); RN(pwritev); RN(aio_write);
    RN(fsync); RN(fdatasync); RN(fcntl); RN(ioctl); RN(syscall);
    RN(guarded_pwrite_np); RN(guarded_write_np);
    RN(dispatch_io_create); RN(dispatch_io_create_with_path); RN(dispatch_io_create_f);
    RN(dispatch_io_write); RN(dispatch_io_read); RN(dispatch_io_close);
    RN(IOServiceOpen); RN(IOServiceGetMatchingService);
    RN(IOConnectCallMethod); RN(IOConnectCallAsyncMethod);
    RN(IOConnectCallStructMethod); RN(IOConnectCallScalarMethod);
    RN(IOConnectMapMemory); RN(IOConnectMapMemory64);
    // RTLD_NEXT from inside an interposer can resolve to the interposer's
    // own symbol — a real_ pointer equal to ts_ is infinite recursion.
#define SAN(f) if ((void *)real_##f == (void *)ts_##f) real_##f = NULL
    SAN(open); SAN(openat); SAN(write); SAN(pwrite); SAN(writev); SAN(pwritev);
    SAN(aio_write); SAN(fsync); SAN(fdatasync); SAN(fcntl); SAN(ioctl); SAN(syscall);
    SAN(guarded_pwrite_np); SAN(guarded_write_np);
    if ((void *)real_dispatch_io_create == (void *)ts_dio_create) real_dispatch_io_create = NULL;
    if ((void *)real_dispatch_io_create_with_path == (void *)ts_dio_create_path) real_dispatch_io_create_with_path = NULL;
    if ((void *)real_dispatch_io_create_f == (void *)ts_dio_create_f) real_dispatch_io_create_f = NULL;
    if ((void *)real_dispatch_io_write == (void *)ts_dio_write) real_dispatch_io_write = NULL;
    if ((void *)real_dispatch_io_read == (void *)ts_dio_read) real_dispatch_io_read = NULL;
    if ((void *)real_dispatch_io_close == (void *)ts_dio_close) real_dispatch_io_close = NULL;
    SAN(IOServiceOpen); SAN(IOServiceGetMatchingService);
    SAN(IOConnectCallMethod); SAN(IOConnectCallAsyncMethod);
    SAN(IOConnectCallStructMethod); SAN(IOConnectCallScalarMethod);
    SAN(IOConnectMapMemory); SAN(IOConnectMapMemory64);
#undef SAN
}

__attribute__((constructor)) static void shim_init(void) {
    ensure_bound();
    const char *lp = getenv("TSLOG");
    if (!lp) lp = "/tmp/tagshift-shim.log";
    char dp[512];
    snprintf(dp, sizeof dp, "%s.data", lp);
    g_logfd = real_open ? real_open(lp, O_WRONLY | O_CREAT | O_APPEND, 0644)
                        : (int)raw_sys6(SYS_open, (long)lp, O_WRONLY | O_CREAT | O_APPEND, 0644, 0, 0, 0);
    g_datafd = real_open ? real_open(dp, O_WRONLY | O_CREAT | O_APPEND, 0644)
                         : (int)raw_sys6(SYS_open, (long)dp, O_WRONLY | O_CREAT | O_APPEND, 0644, 0, 0, 0);
    const char *dev = getenv("TSDEV");
    if (dev) {
        strncpy(g_dev, dev, sizeof g_dev - 1);
        struct stat st;
        if (stat(dev, &st) == 0) g_rdev = st.st_rdev;
    }
    logf_("=== shim loaded pid=%d dev=%s rdev=%d ===\n", getpid(), g_dev, (int)g_rdev);
}

// ---- interpose table --------------------------------------------------------
typedef struct { const void *nu, *old; } ipz_t;
#define IPZ(n, f) __attribute__((used)) static const ipz_t _ipz_##n \
    __attribute__((section("__DATA,__interpose"))) = { (const void *)n, (const void *)f }

IPZ(ts_open, open); IPZ(ts_write, write); IPZ(ts_pwrite, pwrite); IPZ(ts_fsync, fsync);
IPZ(ts_openat, openat);
IPZ(ts_writev, writev); IPZ(ts_pwritev, pwritev); IPZ(ts_aio_write, aio_write);
IPZ(ts_fdatasync, fdatasync);
// fcntl/ioctl/syscall/guarded deliberately NOT interposed — syscall()
// interposition wedges XPC/dispatch once-init (SIGTRAP observed).
#define GROUP_B 1
#ifdef GROUP_B
IPZ(ts_dio_create, dispatch_io_create); IPZ(ts_dio_create_path, dispatch_io_create_with_path);
IPZ(ts_dio_create_f, dispatch_io_create_f);
IPZ(ts_dio_write, dispatch_io_write); IPZ(ts_dio_read, dispatch_io_read);
IPZ(ts_dio_close, dispatch_io_close);
IPZ(ts_IOServiceOpen, IOServiceOpen);
IPZ(ts_IOServiceGetMatchingService, IOServiceGetMatchingService);
IPZ(ts_IOConnectCallMethod, IOConnectCallMethod);
IPZ(ts_IOConnectCallAsyncMethod, IOConnectCallAsyncMethod);
IPZ(ts_IOConnectCallStructMethod, IOConnectCallStructMethod);
IPZ(ts_IOConnectCallScalarMethod, IOConnectCallScalarMethod);
IPZ(ts_IOConnectMapMemory, IOConnectMapMemory); IPZ(ts_IOConnectMapMemory64, IOConnectMapMemory64);
#endif
