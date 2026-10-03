/// Main-thread host state, independent of VZ. A late start completion must
/// never undo a close request, and every exit source shares one stop request.
public final class HostLifecycle {
    public enum State: Equatable {
        case starting, running, failed(String), stopping, stopped
    }

    public private(set) var state: State = .starting
    public private(set) var exitCode: Int32 = 0

    public init() {}

    public func didStart() {
        guard state == .starting else { return }
        state = .running
    }

    public func fail(_ message: String) {
        guard state != .stopping, state != .stopped else { return }
        exitCode = 1
        state = .failed(message)
    }

    @discardableResult
    public func requestStop(code: Int32 = 0) -> Bool {
        guard state != .stopping, state != .stopped else { return false }
        if exitCode == 0 { exitCode = code }
        state = .stopping
        return true
    }

    public func didStop() { state = .stopped }
}
