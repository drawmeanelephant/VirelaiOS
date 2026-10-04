#ifndef QJS_NATIVE_STDLIB_H
#define QJS_NATIVE_STDLIB_H
#include <stddef.h>
size_t qjs_native_checked_alloca_size(size_t);
#define alloca(size) __builtin_alloca(qjs_native_checked_alloca_size(size))
void *malloc(size_t);
void free(void *);
void *realloc(void *, size_t);
size_t malloc_usable_size(const void *);
int abs(int);
_Noreturn void abort(void);
#endif
