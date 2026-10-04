"""Checked QuickJS-only exclusions. Never modify upstream or SDK originals."""
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
INPUT = ROOT / "user/zig/quickjs"
LOCK = json.loads((INPUT / "lock.json").read_text())


def sha(data):
    return hashlib.sha256(data).hexdigest()


def replace_once(text, before, after):
    if text.count(before) != 1:
        raise ValueError("PatchAnchorDrift: " + before[:80])
    return text.replace(before, after, 1)


def function_body(text, name, replacement):
    """Replace exactly one definition, retaining its original declaration."""
    pattern = re.compile(r"(?m)^(?:static\b|void\b|int\b|JSValue\b|JSContext\b)[^;{}]*?\b"
                         + re.escape(name) + r"\([^;{}]*\)\s*\{")
    matches = list(pattern.finditer(text))
    if len(matches) != 1:
        raise ValueError(f"PatchAnchorDrift: definition {name}: {len(matches)}")
    start = matches[0].end() - 1
    # Braces in comments and quoted literals do not delimit C bodies.
    tokens = re.compile(r'/\*.*?\*/|//[^\n]*|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|[{}]', re.S)
    depth = 0
    for token in tokens.finditer(text, start):
        if token.group() == "{":
            depth += 1
        elif token.group() == "}":
            depth -= 1
            if depth == 0:
                return text[:start] + "{\n" + replacement + "\n}" + text[token.end():]
    raise ValueError("PatchAnchorDrift: unterminated " + name)


def section(text, begin, end, replacement=""):
    if text.count(begin) != 1 or text.count(end) != 1:
        raise ValueError("PatchAnchorDrift: section")
    first, last = text.index(begin), text.index(end)
    if first >= last:
        raise ValueError("PatchAnchorDrift: section order")
    return text[:first] + replacement + text[last:]


def core(text):
    for include in ("sys/time.h", "time.h", "fenv.h"):
        text = replace_once(text, f"#include <{include}>\n", "")
    text = replace_once(text, "#include <stdlib.h>\n",
                        '#include <stdlib.h>\n#include "native-boundary.h"\n')
    text = section(text, "#if defined(__APPLE__)\n#include <malloc/malloc.h>",
                   '#include "cutils.h"')
    text = function_body(text, "js_def_malloc_usable_size",
                        "    return malloc_usable_size(ptr);")
    text = replace_once(text, "#define CONFIG_ATOMICS", "/* Native QJS has no Atomics. */")
    text = replace_once(text, "#define DIRECT_DISPATCH  1", "#define DIRECT_DISPATCH  0")
    text = replace_once(text, "    return unlikely(sp < rt->stack_limit);",
                        "    if (unlikely(sp < rt->stack_limit)) {\n"
                        "        qjs_native_stack_failure();\n        qjs_native_step();\n"
                        "        return TRUE;\n    }\n    return FALSE;")
    text = replace_once(text, "    sp = js_get_stack_pointer() - alloca_size;",
                        "    qjs_native_check_alloca(alloca_size);\n"
                        "    sp = js_get_stack_pointer() - alloca_size;")
    text = replace_once(text, "#define JS_INTERRUPT_COUNTER_INIT 10000",
                        "#define JS_INTERRUPT_COUNTER_INIT 1000")
    text = replace_once(text, "static inline __exception int js_poll_interrupts(JSContext *ctx)\n{\n",
                        "static inline __exception int js_poll_interrupts(JSContext *ctx)\n{\n"
                        "    if (qjs_native_charge_interrupt_site()) { qjs_native_step(); return -1; }\n")
    text = function_body(text, "js_random_init", "    ctx->random_state = 1;")
    text = section(text, "/* Date */\n\n/* OS dependent.", "/* RegExp */")
    text = section(text, "/* Date */\n\nstatic int64_t math_mod", "/* eval */",
                   "/* Date is excluded from the native translation unit. */\n\n")
    text = function_body(text, "JS_NewContext",
                        "    qjs_native_unsupported_feature();\n    return NULL;")
    # JS_PrintValue's inspection helper never gains a Date implementation.
    before = "static JSValue get_date_string(JSContext *ctx, JSValueConst this_val,\n                               int argc, JSValueConst *argv, int magic);"
    after = ("static JSValue get_date_string(JSContext *ctx, JSValueConst this_val,\n"
             "                               int argc, JSValueConst *argv, int magic)\n"
             '{ return JS_ThrowTypeError(ctx, "UnsupportedFeature: Date"); }')
    text = replace_once(text, before, after)
    diagnostics = (
        "JS_DumpString", "JS_DumpAtoms", "JS_DumpMemoryUsage",
        "JS_DumpShape", "JS_DumpShapes", "JS_DumpAtom", "JS_DumpValue",
        "JS_DumpValueRT", "JS_DumpObjectHeader", "JS_DumpObject", "JS_DumpGCObject",
        "print_atom", "dump_token", "dump_byte_code",
    )
    for name in diagnostics:
        text = function_body(text, name, "    qjs_native_hosted_diagnostic();")
    # Refuse unsupported syntax at the engine parser, including nested eval
    # and Function constructors. No lexical filter can safely substitute here.
    text = replace_once(text, "    fd->func_kind = func_kind;\n",
                        "    if (func_kind & JS_FUNC_ASYNC) qjs_native_refuse_feature();\n"
                        "    fd->func_kind = func_kind;\n")
    text = replace_once(text, "    eval_type = flags & JS_EVAL_TYPE_MASK;\n    m = NULL;",
                        "    eval_type = flags & JS_EVAL_TYPE_MASK;\n"
                        "    if (eval_type == JS_EVAL_TYPE_MODULE || (flags & JS_EVAL_FLAG_ASYNC))\n"
                        "        qjs_native_refuse_feature();\n    m = NULL;")
    text = function_body(text, "js_parse_import", "    qjs_native_refuse_feature();\n    return -1;")
    text = replace_once(text, "    } else if (s->token.val == TOK_EXPORT && fd->module) {",
                        "    } else if (s->token.val == TOK_IMPORT || s->token.val == TOK_EXPORT) {\n"
                        "        qjs_native_refuse_feature();\n        return -1;\n"
                        "    } else if (s->token.val == TOK_EXPORT && fd->module) {")
    text = replace_once(text, "    case TOK_IMPORT:\n        if (next_token(s))",
                        "    case TOK_IMPORT:\n        qjs_native_refuse_feature();\n"
                        "        if (next_token(s))")
    text = function_body(text, "js_dynamic_import",
                        "    qjs_native_refuse_feature();\n    return JS_EXCEPTION;")
    text = function_body(text, "JS_ExecutePendingJob",
                        "    qjs_native_refuse_feature();\n    return -1;")
    text = function_body(text, "JS_ReadObject",
                        "    qjs_native_refuse_feature();\n    return JS_EXCEPTION;")
    # The remaining diagnostic callback can use only the approved fwrite stub.
    # Disabled DUMP branches must remain disabled; no stdio global is declared.
    if re.search(r"(?m)^#define\s+(DUMP_|TEST|FORCE_GC_|OPCODE_ASM_LABEL)", text):
        raise ValueError("UnsupportedDebugConfiguration")
    return text


def materialize(work):
    expected_headers = {Path(name).name for name in LOCK["private_inputs"]
                        if name.startswith("user/zig/quickjs/headers/")}
    if {path.name for path in (ROOT / "user/zig/quickjs/headers").iterdir()} != expected_headers:
        raise ValueError("SourceDrift: unexpected private header")
    if not (INPUT / "upstream").is_dir():
        raise ValueError("MissingPinnedSource: upstream")
    if {path.name for path in (INPUT / "upstream").iterdir()} - set(LOCK["upstream"]["files"]):
        raise ValueError("SourceDrift: unexpected upstream input")
    for name, digest in LOCK["patches"].items():
        path = ROOT / name
        if path.is_symlink() or not path.is_file() or sha(path.read_bytes()) != digest:
            raise ValueError("SourceDrift: patch " + name)
    for name, digest in LOCK["private_inputs"].items():
        path = ROOT / name
        if path.is_symlink() or not path.is_file() or sha(path.read_bytes()) != digest:
            raise ValueError("SourceDrift: private input " + name)
    manifest = (INPUT / "upstream.sha256").read_bytes()
    if sha(manifest) != LOCK["upstream"]["manifest_sha256"]:
        raise ValueError("SourceDrift: upstream manifest")
    source = work / "source"
    source.mkdir(parents=True, exist_ok=True)
    generated = {name + suffix for name in LOCK["translation_units"]
                 for suffix in (".preprocessed", ".safety.json")}
    if {path.name for path in source.iterdir()} - set(LOCK["upstream"]["files"]) - generated:
        raise ValueError("SourceDrift: unexpected materialized input")
    for name, digest in LOCK["upstream"]["files"].items():
        original = INPUT / "upstream" / name
        if original.is_symlink() or not original.is_file():
            raise ValueError("MissingPinnedSource: " + name)
        data = original.read_bytes()
        if sha(data) != digest:
            raise ValueError("SourceDrift: " + name)
        text = data.decode()
        if name == "quickjs.c":
            text = core(text)
        elif name == "dtoa.c":
            for include in ("ctype.h", "sys/time.h", "setjmp.h"):
                text = replace_once(text, f"#include <{include}>\n", "")
        elif name == "quickjs.h":
            text = replace_once(text, "void JS_DumpMemoryUsage(FILE *fp, const JSMemoryUsage *s, JSRuntime *rt);\n", "")
        elif name == "libregexp.c":
            text = replace_once(text, "#define INTERRUPT_COUNTER_INIT 10000",
                                "#define INTERRUPT_COUNTER_INIT 1000")
        (source / name).write_bytes(text.encode())
    return source
