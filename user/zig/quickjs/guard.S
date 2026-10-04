/* Private AAPCS64 nonlocal refusal boundary, not setjmp/POSIX or an SDK ABI.
 * The boundary is entered and restored in C. No live Zig allocator frame is
 * bypassed: poll callbacks return to C before a refusal restores this frame.
 * x18 is never touched. AAPCS64 preserves the low 64 bits of v8-v15.
 */
.text
#ifdef __APPLE__
#define SYM(name) _##name
#else
#define SYM(name) name
#endif
.globl SYM(qjs_guard_save)
#ifndef __APPLE__
.type qjs_guard_save,@function
#endif
SYM(qjs_guard_save):
    stp x19, x20, [x0, #0]
    stp x21, x22, [x0, #16]
    stp x23, x24, [x0, #32]
    stp x25, x26, [x0, #48]
    stp x27, x28, [x0, #64]
    stp x29, x30, [x0, #80]
    mov x1, sp
    str x1, [x0, #96]
    stp d8, d9, [x0, #112]
    stp d10, d11, [x0, #128]
    stp d12, d13, [x0, #144]
    stp d14, d15, [x0, #160]
    mov w0, #0
    ret
#ifndef __APPLE__
.size qjs_guard_save, .-qjs_guard_save
#endif

.globl SYM(qjs_guard_restore)
#ifndef __APPLE__
.type qjs_guard_restore,@function
#endif
SYM(qjs_guard_restore):
    ldp x19, x20, [x0, #0]
    ldp x21, x22, [x0, #16]
    ldp x23, x24, [x0, #32]
    ldp x25, x26, [x0, #48]
    ldp x27, x28, [x0, #64]
    ldp x29, x30, [x0, #80]
    ldr x1, [x0, #96]
    mov sp, x1
    ldp d8, d9, [x0, #112]
    ldp d10, d11, [x0, #128]
    ldp d12, d13, [x0, #144]
    ldp d14, d15, [x0, #160]
    mov w0, #1
    ret
#ifndef __APPLE__
.size qjs_guard_restore, .-qjs_guard_restore
#endif
