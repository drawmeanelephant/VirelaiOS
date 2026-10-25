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
/* ADR 0039 amendment (M200c): the virelai.* host surface. Each call is a
 * complete synchronous operation; NULL/-1 names its refusal through
 * qjs_native_host_failure, never partial data. */
const char *qjs_native_host_read(const char *, size_t, size_t *);
int qjs_native_host_write(const char *, size_t, const char *, size_t);
const char *qjs_native_host_failure(void);
size_t qjs_native_host_arg_count(void);
long qjs_native_host_arg(size_t, char *, size_t);
#endif
