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

/* ADR 0039 amendment (M200c): virelai.* refusals are ordinary JS exceptions
 * whose name/message is the host's named refusal verbatim. They are
 * catchable and never pretend a partial read, write or arg list. */
static JSValue host_error(JSContext *context)
{
    const char *name = qjs_native_host_failure();
    JSValue error = JS_NewError(context);
    if (JS_IsException(error))
        return error;
    JS_SetPropertyStr(context, error, "name", JS_NewString(context, name ? name : "AccessDenied"));
    JS_SetPropertyStr(context, error, "message", JS_NewString(context, name ? name : "AccessDenied"));
    return JS_Throw(context, error);
}

static JSValue virelai_read_file(JSContext *context, JSValueConst self, int count, JSValueConst *values)
{
    (void)self;
    size_t path_len, length = 0;
    qjs_native_step();
    const char *path = JS_ToCStringLen(context, &path_len, count ? values[0] : JS_UNDEFINED);
    if (!path)
        return JS_EXCEPTION;
    const char *data = qjs_native_host_read(path, path_len, &length);
    JS_FreeCString(context, path);
    qjs_native_step();
    if (!data)
        return host_error(context);
    JSValue result = JS_NewStringLen(context, data, length);
    qjs_native_step();
    return result;
}

static JSValue virelai_write_file(JSContext *context, JSValueConst self, int count, JSValueConst *values)
{
    (void)self;
    size_t path_len, data_len;
    qjs_native_step();
    const char *path = JS_ToCStringLen(context, &path_len, count ? values[0] : JS_UNDEFINED);
    if (!path)
        return JS_EXCEPTION;
    const char *data = JS_ToCStringLen(context, &data_len, count > 1 ? values[1] : JS_UNDEFINED);
    if (!data) {
        JS_FreeCString(context, path);
        return JS_EXCEPTION;
    }
    int failed = qjs_native_host_write(path, path_len, data, data_len);
    JS_FreeCString(context, data);
    JS_FreeCString(context, path);
    qjs_native_step();
    if (failed)
        return host_error(context);
    return JS_UNDEFINED;
}

static JSValue virelai_args_get(JSContext *context, JSValueConst self)
{
    (void)self;
    size_t count = qjs_native_host_arg_count();
    JSValue array = JS_NewArray(context);
    if (JS_IsException(array))
        return array;
    for (size_t i = 0; i < count; i++) {
        char buffer[256];
        long length = qjs_native_host_arg(i, buffer, sizeof buffer);
        if (length < 0) {
            JS_FreeValue(context, array);
            return host_error(context);
        }
        JSValue entry = JS_NewStringLen(context, buffer, (size_t)length);
        if (JS_IsException(entry) ||
            JS_SetPropertyUint32(context, array, (uint32_t)i, entry) < 0) {
            JS_FreeValue(context, entry);
            JS_FreeValue(context, array);
            return JS_EXCEPTION;
        }
    }
    qjs_native_step();
    return array;
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
    JSValue virelai = JS_NewObject(context);
    if (JS_IsException(virelai)) {
        JS_FreeValue(context, console);
        JS_FreeValue(context, global);
        goto fail;
    }
    JSAtom args_atom = JS_NewAtom(context, "args");
    int defined = JS_DefinePropertyGetSet(context, virelai, args_atom,
        JS_NewCFunction2(context, (JSCFunction *)virelai_args_get, "args", 0, JS_CFUNC_getter, 0),
        JS_UNDEFINED, JS_PROP_HAS_GET | JS_PROP_HAS_ENUMERABLE | JS_PROP_ENUMERABLE);
    JS_FreeAtom(context, args_atom);
    if (defined < 0 ||
        JS_SetPropertyStr(context, virelai, "readFile", JS_NewCFunction(context, virelai_read_file, "readFile", 1)) < 0 ||
        JS_SetPropertyStr(context, virelai, "writeFile", JS_NewCFunction(context, virelai_write_file, "writeFile", 2)) < 0 ||
        JS_SetPropertyStr(context, global, "virelai", virelai) < 0) {
        JS_FreeValue(context, virelai);
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
