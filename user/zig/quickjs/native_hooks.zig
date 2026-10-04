pub fn invariant() noreturn {
    @import("sdk").runtime.fail("EngineInvariant", 71);
}
