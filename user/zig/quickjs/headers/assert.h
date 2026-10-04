#ifndef QJS_NATIVE_ASSERT_H
#define QJS_NATIVE_ASSERT_H
_Noreturn void qjs_native_assert(void);
#define assert(condition) ((condition) ? (void)0 : qjs_native_assert())
#endif
