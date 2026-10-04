const qjs = @import("qjs");
extern fn abort() callconv(.c) noreturn;
extern fn qjs_fixture_assert_fail() callconv(.c) noreturn;
pub fn main() void {
    _ = qjs;
    if (@import("fatal_options").assertion) {
        qjs_fixture_assert_fail();
    } else abort();
}
