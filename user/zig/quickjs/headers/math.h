#ifndef QJS_NATIVE_MATH_H
#define QJS_NATIVE_MATH_H
#define INFINITY (__builtin_inff())
#define NAN (__builtin_nanf(""))
#define isnan(x) __builtin_isnan(x)
#define isfinite(x) __builtin_isfinite(x)
#define signbit(x) __builtin_signbit(x)
#define M_E 2.71828182845904523536
#define M_LN2 0.69314718055994530942
#define M_LN10 2.30258509299404568402
#define M_LOG2E 1.44269504088896340736
#define M_LOG10E 0.43429448190325182765
#define M_PI 3.14159265358979323846
#define M_PI_2 1.57079632679489661923
#define M_PI_4 0.78539816339744830962
#define M_SQRT2 1.41421356237309504880
#define M_SQRT1_2 0.70710678118654752440
double acos(double);
double acosh(double);
double asin(double);
double asinh(double);
double atan(double);
double atan2(double, double);
double atanh(double);
double cbrt(double);
double ceil(double);
double cos(double);
double cosh(double);
double exp(double);
double expm1(double);
double fabs(double);
double floor(double);
double fmax(double, double);
double fmin(double, double);
double fmod(double, double);
double hypot(double, double);
double log(double);
double log10(double);
double log1p(double);
double log2(double);
long lrint(double);
double modf(double, double *);
double pow(double, double);
double round(double);
double sin(double);
double sinh(double);
double sqrt(double);
double tan(double);
double tanh(double);
double trunc(double);
#endif
