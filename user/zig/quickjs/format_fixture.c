#include <stdio.h>
#include <stdarg.h>
#include <stdint.h>
#include <stddef.h>
#include <string.h>

#define REQUIRE(condition) do { if (!(condition)) return __LINE__; } while (0)

static int through_va(char *out, size_t capacity, const char *format, ...)
{
    va_list arguments;
    va_start(arguments, format);
    int result = vsnprintf(out, capacity, format, arguments);
    va_end(arguments);
    return result;
}

int qjs_format_contract(void)
{
    char out[128];
    REQUIRE(snprintf(out, sizeof(out), "%d %u %x %X %o", -12, 42u, 42u, 42u, 42u) == 15);
    REQUIRE(strcmp(out, "-12 42 2a 2A 52") == 0);
    REQUIRE(snprintf(out, sizeof(out), "%#08x", 42u) == 8);
    REQUIRE(strcmp(out, "0x00002a") == 0);
    REQUIRE(snprintf(out, sizeof(out), "%+08.3d", 12) == 8);
    REQUIRE(strcmp(out, "    +012") == 0);
    REQUIRE(snprintf(out, sizeof(out), "%-8.3u", 12u) == 8);
    REQUIRE(strcmp(out, "012     ") == 0);
    REQUIRE(snprintf(out, sizeof(out), "%#.0o:%.0u", 0u, 0u) == 2);
    REQUIRE(strcmp(out, "0:") == 0);
    REQUIRE(snprintf(out, sizeof(out), "%hhd %hd %ld %lld %zu %td %ju",
                     -1, -2, -3L, -4LL, (size_t)5, (ptrdiff_t)-6, (uintmax_t)7) == 18);
    REQUIRE(strcmp(out, "-1 -2 -3 -4 5 -6 7") == 0);
    REQUIRE(snprintf(out, sizeof(out), "%lld", (long long)INT64_MIN) == 20);
    REQUIRE(strcmp(out, "-9223372036854775808") == 0);
    REQUIRE(through_va(out, sizeof(out), "%.*s:%*s:%%:%c", 3, "abcdef", -4, "x", 'z') == 12);
    REQUIRE(strcmp(out, "abc:x   :%:z") == 0);
    REQUIRE(through_va(out, sizeof(out), "%.*s", -1, "abc") == 3);
    REQUIRE(strcmp(out, "abc") == 0);
    REQUIRE(snprintf(out, 4, "abcdef") == 6);
    REQUIRE(memcmp(out, "abc\0", 4) == 0);
    REQUIRE(snprintf(out, 1, "abc") == 3 && out[0] == 0);
    out[0] = 'z';
    REQUIRE(snprintf(out, 0, "abc") == 3 && out[0] == 'z');
    REQUIRE(snprintf(NULL, 0, "abc%d", 123) == 6);
    REQUIRE(snprintf(out, sizeof(out), "%cX", 0) == 2);
    REQUIRE(out[0] == 0 && out[1] == 'X' && out[2] == 0);
    return 0;
}

int qjs_format_unsupported(void)
{
    char out[8] = "old";
    const char *unsupported = "%n";
    if (snprintf(out, sizeof(out), unsupported, (int *)(uintptr_t)1) >= 0)
        return 1;
    return out[0] != 0;
}
