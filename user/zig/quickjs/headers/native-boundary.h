#ifndef QJS_NATIVE_BOUNDARY_H
#define QJS_NATIVE_BOUNDARY_H
#include <stddef.h>
void qjs_native_hosted_diagnostic(void);
void qjs_native_unsupported_feature(void);
int qjs_native_checkpoint(void);
void qjs_native_step(void);
void qjs_native_refuse_feature(void);
void qjs_native_stack_failure(void);
void qjs_native_out_of_memory(void);
int qjs_native_charge_interrupt_site(void);
void qjs_native_check_alloca(size_t);
#endif
