#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

#define REQUIRE(condition) do { if (!(condition)) return __LINE__; } while (0)

int qjs_bytes_contract(void)
{
    unsigned char bytes[4096], copy[4096];
    REQUIRE(memset(bytes, 0xa5, sizeof(bytes)) == bytes);
    REQUIRE(memcpy(copy, bytes, sizeof(copy)) == copy);
    REQUIRE(memcmp(copy, bytes, sizeof(copy)) == 0);
    copy[4095] = 0;
    REQUIRE(memcmp(bytes, copy, sizeof(copy)) > 0);
    REQUIRE(memcmp(copy, bytes, sizeof(copy)) < 0);
    REQUIRE(memchr(copy, 0, sizeof(copy)) == copy + 4095);
    REQUIRE(memchr(copy, 256, sizeof(copy)) == copy + 4095);
    REQUIRE(memchr(bytes, 0, sizeof(bytes)) == NULL);
    REQUIRE(memcmp(NULL, NULL, 0) == 0);
    REQUIRE(memchr(NULL, 0, 0) == NULL);
    REQUIRE(memcpy(NULL, NULL, 0) == NULL);
    REQUIRE(memset(NULL, 0, 0) == NULL);
    REQUIRE(memmove(NULL, NULL, 0) == NULL);
    for (size_t i = 0; i < sizeof(bytes); i++)
        bytes[i] = (unsigned char)i;
    REQUIRE(memmove(bytes + 1, bytes, sizeof(bytes) - 1) == bytes + 1);
    for (size_t i = 1; i < sizeof(bytes); i++)
        REQUIRE(bytes[i] == (unsigned char)(i - 1));
    REQUIRE(memmove(bytes, bytes + 1, sizeof(bytes) - 1) == bytes);
    for (size_t i = 0; i < sizeof(bytes) - 1; i++)
        REQUIRE(bytes[i] == (unsigned char)i);
    REQUIRE(memmove(bytes, bytes, sizeof(bytes)) == bytes);
    const char word[] = "a\xff" "ba";
    REQUIRE(strlen(word) == 4);
    REQUIRE(strlen("") == 0);
    REQUIRE(strcmp(word, word) == 0);
    REQUIRE(strcmp("\xff", "\x01") > 0);
    REQUIRE(strcmp("", "a") < 0);
    REQUIRE(strchr(word, 'a') == word);
    REQUIRE(strchr(word, 255) == word + 1);
    REQUIRE(strchr(word, 0) == word + 4);
    REQUIRE(strchr(word, 'x') == NULL);
    REQUIRE(strrchr(word, 'a') == word + 3);
    REQUIRE(strrchr(word, 0) == word + 4);
    REQUIRE(strrchr(word, 'x') == NULL);
    REQUIRE(abs(-123) == 123);
    REQUIRE(abs(0) == 0);
    REQUIRE(abs(123) == 123);
    return 0;
}

int qjs_stub_contract(int which)
{
    /* Deliberately invalid argument pointers prove the stub never reads them. */
    const char *bad = (const char *)(uintptr_t)1;
    FILE *stream = (FILE *)(uintptr_t)1;
    switch (which) {
    case 0: return printf(bad) < 0 ? 0 : 1;
    case 1: return fprintf(stream, bad) < 0 ? 0 : 1;
    case 2: return fputc('x', stream) == EOF ? 0 : 1;
    case 3: return fwrite(bad, SIZE_MAX, SIZE_MAX, stream) == 0 ? 0 : 1;
    case 4: return putchar('x') == EOF ? 0 : 1;
    default: return 1;
    }
}
