#include <stddef.h>
#include <stdint.h>
#include "native-boundary.h"
#include "guard.h"

static QjsGuard *current;
static size_t high_water;

void qjs_guard_enter(QjsGuard *guard)
{
    if (current)
        abort();
    current = guard;
    guard->top = qjs_guard_sp();
    /* Leave 8 KiB for the largest checked C prologue before its entry poll.
     * The builder rejects larger static frames. Dynamic alloca is checked
     * prospectively below, never after crossing the stack mapping. */
    guard->floor = guard->top - (98304 - 8192);
}

void qjs_guard_leave(void)
{
    current = NULL;
}

void qjs_native_step(void)
{
    if (!current)
        return;
    uintptr_t sp = qjs_guard_sp();
    if (current->top > sp && current->top - sp > high_water)
        high_water = current->top - sp;
    if (sp < current->floor) {
        qjs_native_stack_failure();
        qjs_guard_restore(current->saved);
    }
    if (qjs_native_checkpoint())
        qjs_guard_restore(current->saved);
}

void qjs_native_check_alloca(size_t bytes)
{
    if (!current)
        return;
    uintptr_t sp = qjs_guard_sp();
    if (sp < current->floor || bytes > sp - current->floor) {
        qjs_native_stack_failure();
        qjs_guard_restore(current->saved);
    }
}

size_t qjs_guard_high_water(void)
{
    return high_water;
}

size_t qjs_native_checked_alloca_size(size_t bytes)
{
    qjs_native_check_alloca(bytes);
    return bytes;
}

void qjs_native_refuse_feature(void)
{
    qjs_native_unsupported_feature();
    qjs_native_step();
}
