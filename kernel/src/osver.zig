//! M115f (#2465): the OS version label `sysinfo` reports. The value is a
//! build stamp (-Dos-version; build.zig's kernel_options) — but
//! build_options is unreachable from monitor.zig because bare
//! `zig test kernel/src/shell.zig` compiles it with no options module, so
//! the label lives here as a plain global the kernel entry sets before the
//! monitor ever runs.
//!
//! Default is "dev", matching build.zig's -Dos-version default — a kernel
//! built without the option (or a host test that never reaches the entry)
//! reports the same label either way.

pub var label: []const u8 = "dev";

/// M204a (#3463): the kernel image's product version — the `ver M.m.p`
/// the component registry row shows and the drift check resolves
/// (docs/component-registry.md). Unlike `label`, which is a build stamp,
/// this is a source-level const: the bump rule in the registry's
/// version rule says when it moves.
pub const version = "1.0.0";
