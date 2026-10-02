//! Type integration only. This namespace is NOT a POSIX implementation.
const std = @import("std");
comptime {
    const builtin = @import("builtin");
    if (builtin.os.tag == .freestanding) {
        if (builtin.cpu.arch != .aarch64 or builtin.link_libc)
            @compileError("VirelaiNativeTargetRequired: AArch64 freestanding without libc");
        if (!builtin.single_threaded)
            @compileError("VirelaiSingleThreadedRequired: compile with -fsingle-threaded");
    }
}
pub const posix = struct {
    // SDK tokens, never raw kernel fds. Standard streams cannot alias 0..7.
    pub const fd_t = i64;
    pub const pid_t = u64;
    pub const uid_t = u32;
    pub const gid_t = u32;
    pub const ino_t = u64;
    pub const nlink_t = u64;
    pub const mode_t = u16;
    pub const blksize_t = u32;
    pub const STDIN_FILENO: fd_t = -1;
    pub const STDOUT_FILENO: fd_t = -2;
    pub const STDERR_FILENO: fd_t = -3;
    pub const IFNAMESIZE = 16; // storage only; interface lookup is refused
    pub const NAME_MAX = @import("fs.zig").name_max;
    pub const PATH_MAX = @import("fs.zig").path_max;
    pub const SIG = enum(u8) { _ };
    pub const PROT = @compileError("VirelaiUnsupportedPosix: use the SDK arena, not POSIX mmap");
    pub const MREMAP = @compileError("VirelaiUnsupportedPosix: use the SDK arena, not POSIX mremap");
    pub const MAP = @compileError("VirelaiUnsupportedPosix: use the SDK arena, not POSIX mmap");
    pub const E = @compileError("VirelaiUnsupportedPosix: native errors are not POSIX errno");
    pub const errno = @compileError("VirelaiUnsupportedPosix: native errors are not POSIX errno");
    pub const IOV_MAX = @compileError("VirelaiUnsupportedThreaded: use sdk.io");
    pub const getrandom = @compileError("VirelaiUnsupportedPosix: use Io.randomSecure(sdk.io)");
};

/// Only kernel-selected defaults can be requested at create time. No POSIX
/// mode/umask translation or fabricated metadata. Permission changes refuse.
pub const Permissions = enum {
    default_file,
    default_dir,
};

pub const cwd_token = -4;
pub const diagnostic_token = -5;
pub fn cwd() std.Io.Dir {
    return .{ .handle = cwd_token };
}

pub fn exit(status: u8) noreturn {
    @import("runtime.zig").finish(status);
}
pub fn abort() noreturn {
    @import("runtime.zig").fail("Aborted", 71);
}
