const std = @import("std");

pub const limits = struct {
    pub const arena = 4_194_304;
    pub const source = 1_048_576;
    pub const output = 65_536;
    pub const c_stack = 98_304;
    pub const stack = 131_072;
    pub const deadline_ms = 4_000;
    pub const work = 10_000_000;
    pub const line = 4_096;
    pub const evals = 256;
    pub const idle_ms = 60_000;
    pub const session_output = 1_048_576;
    pub const session_source = 4_194_304;
    pub const diagnostics = 16_384;
    pub const terminal_diagnostic = 256;
    pub const receipt = 1_114_112;
    pub const files = 2;
    pub const resources = 7;
    pub const filename = 64;
};

pub const Error = error{
    ContextLimit,
    OutOfMemory,
    InputLimit,
    InvalidUtf8,
    InvalidFilename,
    EvaluationDeadline,
    Cancelled,
    StackLimit,
    WorkLimit,
    OutputLimit,
    UnsupportedHostedDiagnostic,
    UnsupportedFeature,
    JSException,
    ClockUnavailable,
    WriteFailed,
    SessionLimit,
    DiagnosticLimit,
    ReceiptLimit,
    LineLimit,
    IdleLimit,
    HandleLimit,
};

pub const Clock = struct {
    context: *anyopaque,
    frequency: u64,
    counter: *const fn (*anyopaque) u64,

    pub fn validate(self: Clock) Error!void {
        if (self.frequency == 0 or self.frequency > std.math.maxInt(u64) / 60)
            return error.ClockUnavailable;
    }
};

pub const Control = struct {
    clock: Clock,
    failure: ?Error = null,
    cancelled: bool = false,
    running: bool = false,
    cleaning: bool = false,
    start: u64 = 0,
    work_sites: u64 = 0,
    output: usize = 0,
    evals: usize = 0,
    session_source: usize = 0,
    session_output: usize = 0,
    diagnostic_bytes: usize = 0,
    receipt_bytes: usize = 0,
    polls: usize = 0,
    last_poll: u64 = 0,
    maximum_unchecked_ticks: u64 = 0,

    pub fn init(clock: Clock) Error!Control {
        try clock.validate();
        return .{ .clock = clock, .last_poll = clock.counter(clock.context) };
    }

    pub fn refuse(self: *Control, failure: Error) void {
        if (self.failure == null) self.failure = failure;
    }

    pub fn begin(self: *Control, source_bytes: usize) Error!void {
        if (self.running) return error.ContextLimit;
        if (self.failure) |failure| return failure;
        if (source_bytes > limits.source) return error.InputLimit;
        if (self.evals == limits.evals or source_bytes > limits.session_source - self.session_source)
            return error.SessionLimit;
        if (self.diagnostic_bytes > limits.diagnostics - limits.terminal_diagnostic)
            return error.DiagnosticLimit;
        self.failure = null;
        self.output = 0;
        self.work_sites = 0;
        self.maximum_unchecked_ticks = 0;
        self.start = self.clock.counter(self.clock.context);
        self.last_poll = self.start;
        self.evals += 1;
        self.session_source += source_bytes;
        self.running = true;
        self.cleaning = false;
        if (self.cancelled) self.refuse(error.Cancelled);
    }

    pub fn finish(self: *Control) void {
        self.running = false;
    }

    pub fn poll(self: *Control) bool {
        const now = self.clock.counter(self.clock.context);
        self.maximum_unchecked_ticks = @max(self.maximum_unchecked_ticks, now -% self.last_poll);
        self.last_poll = now;
        self.polls += 1;
        // Teardown is noncancellable but remains in the measured poll/timing
        // envelope. It cannot turn a latched failure into a success.
        if (self.cleaning) return true;
        if (self.running) {
            if (self.cancelled) self.refuse(error.Cancelled);
            if (now -% self.start >= self.clock.frequency * 4)
                self.refuse(error.EvaluationDeadline);
        }
        return self.failure == null;
    }

    pub fn site(self: *Control) bool {
        if (self.cleaning) return self.poll();
        if (self.running) {
            if (self.work_sites == limits.work) self.refuse(error.WorkLimit) else self.work_sites += 1;
        }
        return self.poll();
    }

    pub fn cancel(self: *Control) void {
        self.cancelled = true;
        if (self.running) self.refuse(error.Cancelled);
    }

    pub fn chargeOutput(self: *Control, bytes: usize) bool {
        if (!self.poll()) return false;
        if (bytes > limits.output - self.output) self.refuse(error.OutputLimit) else if (bytes > limits.session_output - self.session_output)
            self.refuse(error.SessionLimit)
        else {
            self.output += bytes;
            self.session_output += bytes;
        }
        return self.failure == null;
    }

    pub fn chargeDiagnostic(self: *Control, bytes: usize) Error!void {
        if (bytes > limits.diagnostics - limits.terminal_diagnostic -| self.diagnostic_bytes)
            return error.DiagnosticLimit;
        self.diagnostic_bytes += bytes;
    }

    pub fn chargeReceipt(self: *Control, bytes: usize) Error!void {
        if (bytes > limits.receipt - self.receipt_bytes) return error.ReceiptLimit;
        self.receipt_bytes += bytes;
    }

    pub fn checkLine(_: *Control, bytes: usize) Error!void {
        if (bytes > limits.line) return error.LineLimit;
    }

    pub fn checkIdle(self: *Control, idle_start: u64) Error!void {
        if (self.clock.counter(self.clock.context) -% idle_start >= self.clock.frequency * 60)
            return error.IdleLimit;
    }

    pub fn recovered(self: *Control) void {
        self.failure = null;
        self.cancelled = false;
        self.cleaning = false;
        self.running = false;
    }
};

test "source, session, diagnostics and receipt exact/over boundaries" {
    const Fake = struct {
        fn count(_: *anyopaque) u64 {
            return 0;
        }
    };
    var token: u8 = 0;
    var state = try Control.init(.{ .context = &token, .frequency = 1, .counter = Fake.count });
    try std.testing.expectError(error.InputLimit, state.begin(limits.source + 1));
    try std.testing.expectEqual(@as(usize, 0), state.evals);
    for (0..4) |_| {
        try state.begin(limits.source);
        state.finish();
    }
    try std.testing.expectError(error.SessionLimit, state.begin(1));
    try state.chargeDiagnostic(limits.diagnostics - limits.terminal_diagnostic);
    try std.testing.expectError(error.DiagnosticLimit, state.chargeDiagnostic(1));
    try state.chargeReceipt(limits.receipt);
    try std.testing.expectError(error.ReceiptLimit, state.chargeReceipt(1));
}

test "deadline uses counter frequency, cancellation and refusal are sticky" {
    const Fake = struct {
        tick: u64 = 0,
        fn count(context: *anyopaque) u64 {
            const self: *@This() = @ptrCast(@alignCast(context));
            return self.tick;
        }
    };
    var clock: Fake = .{};
    var state = try Control.init(.{ .context = &clock, .frequency = 24_000_000, .counter = Fake.count });
    try state.begin(1);
    clock.tick = 95_999_999;
    try std.testing.expect(state.poll());
    clock.tick = 96_000_000;
    try std.testing.expect(!state.poll());
    try std.testing.expectEqual(error.EvaluationDeadline, state.failure.?);
    state.cancel();
    try std.testing.expectEqual(error.EvaluationDeadline, state.failure.?);
    state.finish();
    state.recovered();
    state.cancel();
    try state.begin(1);
    try std.testing.expect(!state.poll());
    try std.testing.expectEqual(error.Cancelled, state.failure.?);
    state.finish();
    state.recovered();
    try state.begin(1);
    try std.testing.expect(state.poll());
}

test "work counts each charged site, not interrupt-handler invocations" {
    const Fake = struct {
        fn count(_: *anyopaque) u64 {
            return 0;
        }
    };
    var token: u8 = 0;
    var state = try Control.init(.{ .context = &token, .frequency = 1, .counter = Fake.count });
    try state.begin(1);
    state.work_sites = limits.work - 1;
    try std.testing.expect(state.site());
    try std.testing.expect(!state.site());
    try std.testing.expectEqual(error.WorkLimit, state.failure.?);
}

test "output charges once and overflow survives subsequent catches/polls" {
    const Fake = struct {
        fn count(_: *anyopaque) u64 {
            return 0;
        }
    };
    var token: u8 = 0;
    var state = try Control.init(.{ .context = &token, .frequency = 1, .counter = Fake.count });
    try state.begin(1);
    try std.testing.expect(state.chargeOutput(limits.output));
    try std.testing.expect(!state.chargeOutput(1));
    try std.testing.expectEqual(limits.output, state.output);
    try std.testing.expectEqual(error.OutputLimit, state.failure.?);
    try std.testing.expect(!state.chargeOutput(0));
    var bad = state.clock;
    bad.frequency = 0;
    try std.testing.expectError(error.ClockUnavailable, Control.init(bad));
}

test "eval, session output, line and idle exact/over boundaries" {
    const Fake = struct {
        tick: u64 = 0,
        fn count(context: *anyopaque) u64 {
            const self: *@This() = @ptrCast(@alignCast(context));
            return self.tick;
        }
    };
    var clock: Fake = .{};
    var state = try Control.init(.{ .context = &clock, .frequency = 24_000_000, .counter = Fake.count });
    try state.checkLine(limits.line);
    try std.testing.expectError(error.LineLimit, state.checkLine(limits.line + 1));
    clock.tick = 1_439_999_999;
    try state.checkIdle(0);
    clock.tick += 1;
    try std.testing.expectError(error.IdleLimit, state.checkIdle(0));
    for (0..256) |_| {
        try state.begin(0);
        state.finish();
    }
    try std.testing.expectError(error.SessionLimit, state.begin(0));
    state = try Control.init(state.clock);
    for (0..16) |_| {
        try state.begin(0);
        try std.testing.expect(state.chargeOutput(limits.output));
        state.finish();
    }
    try state.begin(0);
    try std.testing.expect(!state.chargeOutput(1));
    try std.testing.expectEqual(error.SessionLimit, state.failure.?);
    state.finish();
    try std.testing.expectError(error.SessionLimit, state.begin(0));
}
