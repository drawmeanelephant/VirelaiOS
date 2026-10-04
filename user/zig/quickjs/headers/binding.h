#ifndef QJS_NATIVE_BINDING_H
#define QJS_NATIVE_BINDING_H
#include <stddef.h>
typedef struct QjsEngine QjsEngine;
QjsEngine *qjs_engine_create(void);
int qjs_engine_eval(QjsEngine *, const char *, size_t, const char *, int);
int qjs_engine_destroy(QjsEngine *);
int qjs_native_emit(const char *, size_t, int);
#endif
