/* Private freestanding byte/string and diagnostic bridges for QJS only. */
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "native-boundary.h"

static int checkpoint(size_t index)
{
    /* Small fixed SDK/Zig metadata copies must complete even with a latched
     * refusal. Large operations return to their caller, never jump over a
     * Zig allocator frame; the next C safety site propagates the refusal. */
    return index && (index & 1023) == 0 && qjs_native_checkpoint();
}

void *memchr(const void *buffer, int value, size_t count)
{
    const unsigned char *bytes = buffer;
    for (size_t i = 0; i < count; i++) {
        if (checkpoint(i)) return NULL;
        if (bytes[i] == (unsigned char)value)
            return (void *)(bytes + i);
    }
    return NULL;
}

int memcmp(const void *left, const void *right, size_t count)
{
    const unsigned char *a = left, *b = right;
    for (size_t i = 0; i < count; i++) {
        if (checkpoint(i)) return 0;
        if (a[i] != b[i])
            return a[i] < b[i] ? -1 : 1;
    }
    return 0;
}

void *memcpy(void *restrict destination, const void *restrict source, size_t count)
{
    unsigned char *out = destination;
    const unsigned char *in = source;
    for (size_t i = 0; i < count; i++) {
        if (checkpoint(i)) return destination;
        out[i] = in[i];
    }
    return destination;
}

void *memmove(void *destination, const void *source, size_t count)
{
    unsigned char *out = destination;
    const unsigned char *in = source;
    if ((uintptr_t)out <= (uintptr_t)in) {
        for (size_t i = 0; i < count; i++) {
            if (checkpoint(i)) return destination;
            out[i] = in[i];
        }
    } else {
        for (size_t i = count; i > 0; i--) {
            if (checkpoint(count - i)) return destination;
            out[i - 1] = in[i - 1];
        }
    }
    return destination;
}

void *memset(void *destination, int value, size_t count)
{
    unsigned char *out = destination;
    for (size_t i = 0; i < count; i++) {
        if (checkpoint(i)) return destination;
        out[i] = (unsigned char)value;
    }
    return destination;
}

size_t strlen(const char *string)
{
    size_t length = 0;
    for (;;) {
        if (checkpoint(length)) return length;
        if (!string[length])
            return length;
        length++;
    }
}

int strcmp(const char *left, const char *right)
{
    for (size_t i = 0;; i++) {
        if (checkpoint(i)) return 0;
        unsigned char a = (unsigned char)left[i], b = (unsigned char)right[i];
        if (a != b)
            return a < b ? -1 : 1;
        if (!a)
            return 0;
    }
}

char *strchr(const char *string, int value)
{
    for (size_t i = 0;; i++) {
        if (checkpoint(i)) return NULL;
        unsigned char current = (unsigned char)string[i];
        if (current == (unsigned char)value)
            return (char *)(string + i);
        if (!current)
            return NULL;
    }
}

char *strrchr(const char *string, int value)
{
    const char *last = NULL;
    for (size_t i = 0;; i++) {
        if (checkpoint(i)) return NULL;
        unsigned char current = (unsigned char)string[i];
        if (current == (unsigned char)value)
            last = string + i;
        if (!current)
            return (char *)last;
    }
}

int abs(int value)
{
    /* INT_MIN is outside C abs's supported operand contract. */
    return value < 0 ? -value : value;
}

int printf(const char *restrict format, ...)
{
    (void)format;
    qjs_native_hosted_diagnostic();
    return -1;
}

int fprintf(FILE *restrict stream, const char *restrict format, ...)
{
    (void)stream;
    (void)format;
    qjs_native_hosted_diagnostic();
    return -1;
}

int fputc(int value, FILE *stream)
{
    (void)value;
    (void)stream;
    qjs_native_hosted_diagnostic();
    return EOF;
}

size_t fwrite(const void *restrict buffer, size_t size, size_t count, FILE *restrict stream)
{
    (void)buffer;
    (void)size;
    (void)count;
    (void)stream;
    qjs_native_hosted_diagnostic();
    return 0;
}

int putchar(int value)
{
    (void)value;
    qjs_native_hosted_diagnostic();
    return EOF;
}
