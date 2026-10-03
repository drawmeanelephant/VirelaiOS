// Foundation's run loop keeps VM timers alive, but does not
// dispatch native AppKit input. Only display+input sessions need that loop;
// screenshot-only and headless gates retain their existing behavior.

public enum NativeInput {
    public static func enabled(display: Bool, input: Bool) -> Bool {
        display && input
    }

    public static func run(
        display: Bool,
        input: Bool,
        applicationLoop: () -> Void,
        headlessLoop: () -> Void
    ) {
        if enabled(display: display, input: input) {
            applicationLoop()
        } else {
            headlessLoop()
        }
    }
}
