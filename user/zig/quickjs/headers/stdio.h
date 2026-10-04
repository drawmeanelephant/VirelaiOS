#ifndef QJS_NATIVE_STDIO_H
#define QJS_NATIVE_STDIO_H
#include <stddef.h>
#include <stdarg.h>
/* An opaque diagnostic argument only, never a file object or descriptor. */
typedef struct QJSUnsupportedDiagnostic FILE;
#define EOF (-1)
int snprintf(char *restrict, size_t, const char *restrict, ...)
    __attribute__((format(printf, 3, 4)));
int vsnprintf(char *restrict, size_t, const char *restrict, va_list)
    __attribute__((format(printf, 3, 0)));
int printf(const char *restrict, ...);
int fprintf(FILE *restrict, const char *restrict, ...);
int fputc(int, FILE *);
size_t fwrite(const void *restrict, size_t, size_t, FILE *restrict);
int putchar(int);
#endif
