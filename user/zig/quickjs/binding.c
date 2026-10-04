#include <stdlib.h>
#include <string.h>
#include "quickjs.h"
#include "native-boundary.h"
#include "guard.h"
#include "binding.h"

struct QjsEngine {
    JSRuntime *runtime;
    JSContext *context;
};

static void *allocate(JSMallocState *state, size_t bytes)
{
    void *ptr = malloc(bytes);
    if (ptr) {
        state->malloc_count++;
        state->malloc_size += malloc_usable_size(ptr);
    } else qjs_native_out_of_memory();
    return ptr;
}

static void release(JSMallocState *state, void *ptr)
{
    if (ptr) {
        state->malloc_count--;
        state->malloc_size -= malloc_usable_size(ptr);
        free(ptr);
    }
}

static void *resize(JSMallocState *state, void *ptr, size_t bytes)
{
    if (!ptr)
        return allocate(state, bytes);
    if (!bytes) {
        release(state, ptr);
        return NULL;
    }
    size_t before = malloc_usable_size(ptr);
    void *next = realloc(ptr, bytes);
    if (next)
        state->malloc_size = state->malloc_size - before + malloc_usable_size(next);
    else qjs_native_out_of_memory();
    return next;
}

static int interrupted(JSRuntime *runtime, void *opaque)
{
    (void)runtime;
    (void)opaque;
    return qjs_native_checkpoint();
}

static JSValue print(JSContext *context, JSValueConst self, int count, JSValueConst *values)
{
    (void)self;
    for (int i = 0; i < count; i++) {
        qjs_native_step();
        size_t length;
        const char *text = JS_ToCStringLen(context, &length, values[i]);
        if (!text)
            return JS_EXCEPTION;
        if (i && qjs_native_emit(" ", 1, 0)) {
            JS_FreeCString(context, text);
            qjs_native_step();
            return JS_EXCEPTION;
        }
        int failed = qjs_native_emit(text, length, 0);
        JS_FreeCString(context, text);
        if (failed) {
            qjs_native_step();
            return JS_EXCEPTION;
        }
    }
    if (qjs_native_emit("\n", 1, 0)) {
        qjs_native_step();
        return JS_EXCEPTION;
    }
    return JS_UNDEFINED;
}

QjsEngine *qjs_engine_create(void)
{
    QJS_BOUNDARY_OR_RETURN(NULL);
    static const JSMallocFunctions functions = { allocate, release, resize, malloc_usable_size };
    QjsEngine *engine = malloc(sizeof(*engine));
    if (!engine) {
        qjs_guard_leave();
        return NULL;
    }
    engine->runtime = JS_NewRuntime2(&functions, NULL);
    engine->context = NULL;
    if (!engine->runtime) {
        free(engine);
        qjs_guard_leave();
        return NULL;
    }
    JS_SetMemoryLimit(engine->runtime, 4194304);
    JS_SetMaxStackSize(engine->runtime, 98304);
    JS_SetInterruptHandler(engine->runtime, interrupted, NULL);
    engine->context = JS_NewContextRaw(engine->runtime);
    JSContext *context = engine->context;
    if (!context || JS_AddIntrinsicBaseObjects(context) ||
        JS_AddIntrinsicEval(context) || JS_AddIntrinsicStringNormalize(context) ||
        JS_AddIntrinsicRegExp(context) || JS_AddIntrinsicJSON(context) ||
        JS_AddIntrinsicMapSet(context))
        goto fail;
    JSValue global = JS_GetGlobalObject(context);
    JSValue console = JS_NewObject(context);
    if (JS_IsException(console)) {
        JS_FreeValue(context, global);
        goto fail;
    }
    if (JS_SetPropertyStr(context, global, "print", JS_NewCFunction(context, print, "print", 0)) < 0 ||
        JS_SetPropertyStr(context, console, "log", JS_NewCFunction(context, print, "log", 0)) < 0) {
        JS_FreeValue(context, console);
        JS_FreeValue(context, global);
        goto fail;
    }
    int result = JS_SetPropertyStr(context, global, "console", console);
    JS_FreeValue(context, global);
    if (result < 0)
        goto fail;
    qjs_guard_leave();
    return engine;
fail:
    if (engine->context) JS_FreeContext(engine->context);
    JS_FreeRuntime(engine->runtime);
    free(engine);
    qjs_guard_leave();
    return NULL;
}

int qjs_engine_eval(QjsEngine *engine, const char *source, size_t length,
                    const char *filename, int emit_result)
{
    QJS_BOUNDARY_OR_RETURN(-2);
    JSContext *context = engine->context;
    // Only outer entries update the engine stack top.
    JS_UpdateStackTop(engine->runtime);
    JSValue value = JS_Eval(context, source, length, filename, JS_EVAL_TYPE_GLOBAL);
    int result = 0;
    if (JS_IsException(value)) {
        JSValue exception = JS_GetException(context);
        size_t bytes;
        const char *message = JS_ToCStringLen(context, &bytes, exception);
        if (message) {
            // No unbounded stack trace or truncation-success message.
            if (bytes > 256) bytes = 256;
            qjs_native_emit(message, bytes, 1);
            JS_FreeCString(context, message);
        }
        JS_FreeValue(context, exception);
        result = -1;
    } else if (emit_result && !JS_IsUndefined(value)) {
        size_t bytes;
        const char *text = JS_ToCStringLen(context, &bytes, value);
        if (!text) result = -1;
        else {
            if (qjs_native_emit(text, bytes, 0) || qjs_native_emit("\n", 1, 0))
                result = -2;
            JS_FreeCString(context, text);
        }
    }
    JS_FreeValue(context, value);
    qjs_native_step();
    qjs_guard_leave();
    return result;
}

int qjs_engine_destroy(QjsEngine *engine)
{
    if (!engine)
        return 0;
    QJS_BOUNDARY_OR_RETURN(-2);
    JS_FreeContext(engine->context);
    JS_FreeRuntime(engine->runtime);
    free(engine);
    qjs_guard_leave();
    return 0;
}
