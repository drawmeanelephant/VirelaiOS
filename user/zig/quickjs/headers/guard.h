#ifndef QJS_NATIVE_GUARD_H
#define QJS_NATIVE_GUARD_H
#include <stdint.h>
#include <stdlib.h>
typedef struct {
    uint64_t saved[22];
    uintptr_t floor;
    uintptr_t top;
} QjsGuard;
int qjs_guard_save(uint64_t *) __attribute__((returns_twice));
_Noreturn void qjs_guard_restore(uint64_t *);
void qjs_guard_enter(QjsGuard *);
void qjs_guard_leave(void);
size_t qjs_guard_high_water(void);
static inline uintptr_t qjs_guard_sp(void)
{
    uintptr_t value;
    __asm__ volatile("mov %0, sp" : "=r"(value));
    return value;
}
#define QJS_BOUNDARY_OR_RETURN(value) \
    QjsGuard boundary; \
    qjs_guard_enter(&boundary); \
    if (qjs_guard_save(boundary.saved)) { qjs_guard_leave(); return (value); }
#endif
