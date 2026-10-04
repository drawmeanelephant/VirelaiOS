#include <assert.h>
#include <stdlib.h>
_Noreturn void qjs_fixture_assert_fail(void)
{
    assert(0);
    abort();
}
