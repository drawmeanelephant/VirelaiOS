extern fn write(c_int, [*]const u8, usize) callconv(.c) isize;
extern fn _exit(c_int) callconv(.c) noreturn;
pub fn invariant() noreturn {
    const text = "EngineInvariant\n";
    _ = write(2, text, text.len);
    _exit(71);
}
