/* Bounded formatter for the pinned core's non-floating literal formats. */
#include <stdarg.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include "native-boundary.h"

typedef struct {
    char *destination;
    size_t capacity;
    size_t count;
    int failed;
} Writer;

static void put(Writer *writer, char byte)
{
    if (writer->failed)
        return;
    if ((writer->count & 1023) == 0 && qjs_native_checkpoint()) {
        writer->failed = 1;
        return;
    }
    if (writer->count == INT32_MAX) {
        writer->failed = 1;
        return;
    }
    if (writer->capacity && writer->count < writer->capacity - 1)
        writer->destination[writer->count] = byte;
    writer->count++;
}

static void repeat(Writer *writer, char byte, size_t count)
{
    for (size_t i = 0; i < count && !writer->failed; i++)
        put(writer, byte);
}

static int decimal(const char **format, size_t *value)
{
    size_t number = 0;
    while (**format >= '0' && **format <= '9') {
        if (qjs_native_checkpoint())
            return -1;
        unsigned digit = (unsigned)(**format - '0');
        if (number > (INT32_MAX - digit) / 10)
            return -1;
        number = number * 10 + digit;
        (*format)++;
    }
    *value = number;
    return 0;
}

static int unsupported(Writer *writer)
{
    qjs_native_hosted_diagnostic();
    if (writer->capacity && writer->destination)
        writer->destination[0] = 0;
    return -1;
}

int vsnprintf(char *restrict destination, size_t capacity,
              const char *restrict format, va_list arguments)
{
    Writer writer = { destination, capacity, 0, 0 };
    if ((!destination && capacity) || !format)
        return unsupported(&writer);
    while (*format && !writer.failed) {
        if (*format != '%') {
            put(&writer, *format++);
            continue;
        }
        format++;
        if (*format == '%') {
            put(&writer, *format++);
            continue;
        }
        int left = 0, zero = 0, plus = 0, space = 0, alternate = 0;
        for (;;) {
            if (*format == '-') left = 1;
            else if (*format == '0') zero = 1;
            else if (*format == '+') plus = 1;
            else if (*format == ' ') space = 1;
            else if (*format == '#') alternate = 1;
            else break;
            format++;
            if (qjs_native_checkpoint()) {
                writer.failed = 1;
                break;
            }
        }
        if (writer.failed)
            break;
        size_t width = 0, precision = 0;
        int precise = 0;
        if (*format == '*') {
            int signed_width = va_arg(arguments, int);
            format++;
            if (signed_width < 0) {
                left = 1;
                width = 0u - (unsigned)signed_width;
            } else width = (unsigned)signed_width;
            if (width > INT32_MAX)
                return unsupported(&writer);
        } else if (decimal(&format, &width))
            return unsupported(&writer);
        if (*format == '.') {
            format++;
            precise = 1;
            if (*format == '*') {
                int signed_precision = va_arg(arguments, int);
                format++;
                if (signed_precision < 0) precise = 0;
                else precision = (unsigned)signed_precision;
            } else if (decimal(&format, &precision))
                return unsupported(&writer);
        }
        enum { NORMAL, HH, H, L, LL, Z, T, J } length = NORMAL;
        if (*format == 'h') {
            format++;
            length = H;
            if (*format == 'h') { format++; length = HH; }
        } else if (*format == 'l') {
            format++;
            length = L;
            if (*format == 'l') { format++; length = LL; }
        } else if (*format == 'z') { format++; length = Z; }
        else if (*format == 't') { format++; length = T; }
        else if (*format == 'j') { format++; length = J; }
        char conversion = *format++;
        if (!conversion)
            return unsupported(&writer);
        if (conversion == 's' || conversion == 'c') {
            if (length != NORMAL)
                return unsupported(&writer);
            const char *string;
            char character;
            size_t count = 0;
            if (conversion == 'c') {
                character = (char)va_arg(arguments, int);
                string = &character;
                count = 1;
            } else {
                string = va_arg(arguments, const char *);
                if (!string)
                    return unsupported(&writer);
                while ((!precise || count < precision) && string[count]) {
                    if ((count & 1023) == 0 && qjs_native_checkpoint()) {
                        writer.failed = 1;
                        break;
                    }
                    count++;
                }
            }
            size_t padding = width > count ? width - count : 0;
            if (!left) repeat(&writer, ' ', padding);
            for (size_t i = 0; i < count && !writer.failed; i++) put(&writer, string[i]);
            if (left) repeat(&writer, ' ', padding);
            continue;
        }
        int signed_conversion = conversion == 'd' || conversion == 'i';
        unsigned base = conversion == 'o' ? 8 : (conversion == 'x' || conversion == 'X' || conversion == 'p') ? 16 : 10;
        if (!signed_conversion && conversion != 'u' && conversion != 'o' &&
            conversion != 'x' && conversion != 'X' && conversion != 'p')
            return unsupported(&writer);
        uint64_t value;
        char sign = 0;
        if (conversion == 'p') {
            if (length != NORMAL)
                return unsupported(&writer);
            value = (uintptr_t)va_arg(arguments, void *);
        } else if (signed_conversion) {
            int64_t integer;
            switch (length) {
            case HH: integer = (signed char)va_arg(arguments, int); break;
            case H: integer = (short)va_arg(arguments, int); break;
            case L: integer = va_arg(arguments, long); break;
            case LL: integer = va_arg(arguments, long long); break;
            case Z: case T: integer = va_arg(arguments, ptrdiff_t); break;
            case J: integer = va_arg(arguments, intmax_t); break;
            default: integer = va_arg(arguments, int); break;
            }
            value = (uint64_t)integer;
            if (integer < 0) { sign = '-'; value = 0 - value; }
            else if (plus) sign = '+';
            else if (space) sign = ' ';
        } else {
            switch (length) {
            case HH: value = (unsigned char)va_arg(arguments, int); break;
            case H: value = (unsigned short)va_arg(arguments, int); break;
            case L: value = va_arg(arguments, unsigned long); break;
            case LL: value = va_arg(arguments, unsigned long long); break;
            case Z: case T: value = va_arg(arguments, size_t); break;
            case J: value = va_arg(arguments, uintmax_t); break;
            default: value = va_arg(arguments, unsigned); break;
            }
        }
        char digits[64];
        const char *alphabet = conversion == 'X' ? "0123456789ABCDEF" : "0123456789abcdef";
        size_t count = 0;
        uint64_t original = value;
        if (value || !precise || precision)
            do {
                digits[count++] = alphabet[value % base];
                value /= base;
            } while (value);
        char prefix[2];
        size_t prefix_length = 0;
        if (sign) prefix[prefix_length++] = sign;
        if (conversion == 'p' || (alternate && base == 16 && original)) {
            prefix[0] = '0';
            prefix[1] = conversion == 'X' ? 'X' : 'x';
            prefix_length = 2;
        }
        if (alternate && base == 8 && (count == 0 || digits[count - 1] != '0') && precision <= count)
            precision = count + 1;
        size_t zeros = precision > count ? precision - count : 0;
        size_t content = prefix_length + zeros + count;
        size_t padding = width > content ? width - content : 0;
        if (!left && (!zero || precise)) repeat(&writer, ' ', padding);
        for (size_t i = 0; i < prefix_length; i++) put(&writer, prefix[i]);
        if (!left && zero && !precise) repeat(&writer, '0', padding);
        repeat(&writer, '0', zeros);
        while (count && !writer.failed) put(&writer, digits[--count]);
        if (left) repeat(&writer, ' ', padding);
    }
    if (capacity)
        destination[writer.count < capacity ? writer.count : capacity - 1] = 0;
    if (writer.failed) {
        if (capacity) destination[0] = 0;
        return -1;
    }
    return (int)writer.count;
}

int snprintf(char *restrict destination, size_t capacity,
             const char *restrict format, ...)
{
    va_list arguments;
    va_start(arguments, format);
    int result = vsnprintf(destination, capacity, format, arguments);
    va_end(arguments);
    return result;
}
