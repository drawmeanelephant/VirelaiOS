#ifndef QJS_NATIVE_STRING_H
#define QJS_NATIVE_STRING_H
#include <stddef.h>
void *memchr(const void *, int, size_t);
int memcmp(const void *, const void *, size_t);
void *memcpy(void *restrict, const void *restrict, size_t);
void *memmove(void *, const void *, size_t);
void *memset(void *, int, size_t);
char *strchr(const char *, int);
int strcmp(const char *, const char *);
size_t strlen(const char *);
char *strrchr(const char *, int);
#endif
