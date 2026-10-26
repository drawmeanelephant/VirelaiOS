// Console lane: terminal raw mode, serial pipes, guest-output
// tee, stdin forwarding, and the --console-tcp bridge.

import AppKit
import ApplicationServices
import CryptoKit
import Darwin
// DiskImageKit ships in the macOS 27 SDK only (#523 item 2). The class-A
// CI job builds against the older macos-latest SDK, so the import and the
// --overlay-base implementation are compiled in ONLY when the SDK has the
// framework; elsewhere the flag fails honestly at runtime.
#if canImport(DiskImageKit)
import DiskImageKit
#endif
import Foundation
import ScreenCaptureKit
import Security
import Virtualization
// M117d (#2476, ADR 0057 D11): vmnet.framework backs --net-vmnet. The
// vmnet_network_* symbols are macOS 26+, so they exist wherever this
// runner's deployment floor applies; the module-level guard keeps an
// older-SDK toolchain compiling — the flag then refuses by name.
#if canImport(vmnet)
import vmnet
#endif
import VFWire
import VMAppKit
import VMDiskImage
import VMGPU3D
import VMPostmortem
import VMProfile
import VMTcp
import VSCtl
import VSNTP
import VSSH


// ---------------------------------------------------------------------------
// Terminal state management (used in console mode; no-ops elsewhere).
// ---------------------------------------------------------------------------
var originalTermios: termios?

var terminalRaw = false

func setupTerminal() {
    guard consoleMode else { return }
    guard isatty(STDIN_FILENO) == 1 else {
        print("  terminal: stdin is not a TTY — character mode skipped (piped input is still forwarded to the guest)")
        return
    }
    var t = termios()
    guard tcgetattr(STDIN_FILENO, &t) == 0 else {
        print("  terminal: WARNING — could not read terminal settings (tcgetattr failed); terminal left untouched")
        return
    }
    originalTermios = t
    // Character-oriented input: disable canonical line buffering and echo.
    // ISIG stays on so ^C still raises SIGINT (documented: it ends the host
    // console session rather than reaching the guest). ICRNL off so Enter is
    // forwarded as \r (0x0d); IXON off so ^S/^Q pass through to the guest.
    // Backspace is forwarded as the raw byte the terminal sends (typically
    // 0x7f). No host-side line editing is performed.
    t.c_lflag &= ~tcflag_t(ICANON | ECHO)
    t.c_iflag &= ~tcflag_t(ICRNL | IXON)
    withUnsafeMutableBytes(of: &t.c_cc) { raw in
        raw[Int(VMIN)] = 1
        raw[Int(VTIME)] = 0
    }
    guard tcsetattr(STDIN_FILENO, TCSANOW, &t) == 0 else {
        print("  terminal: WARNING — could not apply character mode (tcsetattr failed); input stays canonical")
        originalTermios = nil
        return
    }
    terminalRaw = true
    print("  terminal: character mode enabled (no echo, no line editing; Ctrl-C ends the session)")
}

func restoreTerminal() {
    guard terminalRaw, var orig = originalTermios else { return }
    terminalRaw = false
    originalTermios = nil
    tcsetattr(STDIN_FILENO, TCSANOW, &orig)
}

// Console-mode duplex pipes. The attachment reads host input from
// consoleInputPipe's read end (we forward stdin into its write end) and
// writes guest output to consoleOutputPipe's write end (we tee its read end
// to the terminal and the log). Non-console mode never touches these.
let consoleInputPipe = Pipe()

let consoleOutputPipe = Pipe()

var serialLogHandle: FileHandle?

let serialURL = URL(fileURLWithPath: serialLogPath)

// Claim 0680: the structured-console sink. Opened when --cvc-console-file
// was passed; the delegate writes every queue-1 guest log line into it
// verbatim (raw bytes, no injected newlines — the guest's own '\n'
// terminators are the only line breaks, so the file stays byte-faithful).
var cvcConsoleFileHandle: FileHandle?

let serialConfig = VZVirtioConsoleDeviceSerialPortConfiguration()

// ---------------------------------------------------------------------------
// Console mode: streaming tee, stdin forwarding, signal-safe exit.
// ---------------------------------------------------------------------------
// Claim 2188: teardown serial drain. Exit paths used to sleep a fixed
// 0.4–0.5 s and hope the guest-output tee finished writing the pipe before
// exit() cut it off; the sleeps could not wait for pipe EOF because VZ
// keeps its copy of the write end open until process exit (observed).
// Instead the tee now runs a poll loop and stops on REQUEST: after the VM
// stops (every guest byte VZ will deliver is already in the pipe), the
// drain sets the stop flag and waits on `teeGroup` until the tee has
// flushed the pipe to empty and synchronized the log — so the LAST guest
// bytes always land before exit(). `teeRunning` is false in evidence mode
// (no duplex pipe there — VZ writes the serial log directly), so the drain
// is a structural no-op for evidence runs.
let teeGroup = DispatchGroup()

var teeRunning = false

// Claim 2188: the tee's poll loop checks this flag instead of waiting for
// pipe EOF — VZ keeps its copy of the pipe's write end open until process
// exit, so EOF never arrives at stop time. The flag is set only AFTER the
// VM has stopped, when every guest byte VZ will deliver is already in the
// pipe; the tee then drains the pipe to empty and signals `teeGroup`.
let teeStopLock = NSLock()

var teeStopRequested = false

func requestTeeStop() {
    teeStopLock.lock()
    teeStopRequested = true
    teeStopLock.unlock()
}

func teeStopIsRequested() -> Bool {
    teeStopLock.lock()
    defer { teeStopLock.unlock() }
    return teeStopRequested
}

/// Bounded wait for the guest-output tee to drain the pipe and flush the
/// serial log. Call AFTER the VM has stopped (or its serial attachment
/// closed): the tee then flushes the pipe to empty and signals the group.
/// No-op when no tee is running (evidence mode — VZ writes the log
/// directly).
func drainTeeBeforeExit(_ context: String, timeout: TimeInterval = 3.0) {
    // M139a: a run never exits with a sibling VM still flying — every
    // exit path drains the session fleet first (bounded).
    VMSessionRegistry.shared.shutdownAllAndWait()
    guard teeRunning else { return }
    requestTeeStop()
    if teeGroup.wait(timeout: .now() + timeout) == .timedOut {
        FileHandle.standardError.write(Data("WARNING: \(context): guest-output tee did not drain within \(Int(timeout))s — some tail bytes may be missing from the serial log\n".utf8))
    }
}

func startGuestOutputTee() {
    // Guest output → terminal + serial log. Streaming tee: each chunk is
    // written as it arrives; the log is never reloaded to show new bytes.
    let teeQueue = DispatchQueue(label: "virelaios.tee")
    var logWriteWarned = false
    teeRunning = true
    teeGroup.enter()
    teeQueue.async {
        defer { teeGroup.leave() } // the stopping drain finished — the exit path can proceed
        let fh = consoleOutputPipe.fileHandleForReading
        let fd = fh.fileDescriptor
        // Non-blocking reads: the stopping drain empties the pipe without
        // ever blocking on a writer (VZ) that stays open until exit.
        let flags = fcntl(fd, F_GETFL)
        _ = fcntl(fd, F_SETFL, flags | O_NONBLOCK)
        var buf = [UInt8](repeating: 0, count: 4096)
        func emit(_ n: Int) {
            let data = Data(bytes: buf, count: n)
            try? FileHandle.standardOutput.write(contentsOf: data)
            consoleTCPEmit(data)
            do {
                try serialLogHandle?.write(contentsOf: data)
            } catch {
                if !logWriteWarned {
                    logWriteWarned = true
                    FileHandle.standardError.write(Data("WARNING: could not write guest output to \(serialLogPath): \(error)\n".utf8))
                }
            }
        }
        // Read everything currently in the pipe (non-blocking) and emit it.
        // Returns when the pipe is empty (EAGAIN), EOF, or on error.
        func drainPipe() {
            while true {
                var n: Int
                repeat { n = read(fd, &buf, buf.count) } while n < 0 && errno == EINTR
                if n > 0 {
                    emit(n)
                    continue
                }
                break // 0 = EOF, < 0 = EAGAIN or error — pipe drained
            }
        }
        while true {
            if teeStopIsRequested() {
                // Claim 2188: the VM has stopped — every guest byte VZ will
                // deliver is already in the pipe. Flush it to empty, then
                // end so drainTeeBeforeExit returns with the true tail.
                drainPipe()
                break
            }
            var pfd = pollfd(fd: fd, events: Int16(POLLIN), revents: 0)
            let pr = poll(&pfd, 1, 200)
            if pr < 0, errno == EINTR { continue }
            if pr > 0 {
                if (pfd.revents & Int16(POLLIN)) != 0 {
                    drainPipe()
                }
                if (pfd.revents & Int16(POLLHUP | POLLERR)) != 0 {
                    drainPipe() // write end closed — flush and finish
                    break
                }
            }
        }
        try? serialLogHandle?.synchronize()
    }
}

func startStdinForwarding() {
    // Host stdin → guest serial input (raw bytes, character-oriented).
    let inputQueue = DispatchQueue(label: "virelaios.stdin")
    inputQueue.async {
        var buf = [UInt8](repeating: 0, count: 1024)
        while true {
            var n: Int
            repeat { n = read(STDIN_FILENO, &buf, buf.count) } while n < 0 && errno == EINTR
            if n <= 0 { break } // EOF: stop forwarding and close the guest input side
            let data = Data(bytes: buf, count: n)
            if debugInput {
                let hex = data.map { String(format: "%02x", $0) }.joined(separator: " ")
                FileHandle.standardError.write(Data("input → serial attachment: \(data.count) bytes: \(hex)\n".utf8))
            }
            do { try consoleInputPipe.fileHandleForWriting.write(contentsOf: data) }
            catch { break }
        }
        try? consoleInputPipe.fileHandleForWriting.close()
    }
}

func startConsoleStreams() {
    startGuestOutputTee()
    // The stdin forwarder CLOSES the shared serial-input pipe's write end on
    // EOF, which would break a TCP client that connects later. When the TCP
    // console bridge owns input, stdin forwarding stays off.
    if consoleTCPPort == nil {
        startStdinForwarding()
    } else {
        startTCPConsoleBridge(port: consoleTCPPort!, bindHost: consoleTCPBind)
    }
}

// #1066 Stage 0 (issue #1066) + M70g G2 (#1459, ADR 0022 D2 amendment): the
// TCP console bridge. Still ONE client at a time, but the policy is now
// explicit and every open end is bounded:
//   * the accept loop keeps accepting while a client is served and answers
//     every extra connection `console-tcp: busy` + close (it no longer hangs
//     in the listen backlog); serving runs on its own serial queue;
//   * pre-auth reads carry a deadline (a connected-but-silent client — the
//     half-open shape — is dropped with `console-tcp: auth timeout`);
//   * the MAC compare is CryptoKit's constant-time check, never String ==;
//   * post-auth input is bounded per line (`console-tcp: line too long`);
//   * guest output reaches the client through `consoleTCPEmit` (the output
//     tee) under SO_SNDTIMEO — a client that stops reading is dropped, never
//     allowed to stall the tee; SO_KEEPALIVE probes a vanished peer;
//   * a client that dies mid-line has its partial line cancelled in the
//     guest (Ctrl-C — lineedit echoes `^C` and clears) so the next client
//     starts at a clean prompt — M52's no-zombie-seat lesson.
// Only `tcpClientFD`/`tcpClientServing` are shared; both live under the lock.
var tcpClientFD: Int32 = -1

var tcpClientServing = false

let tcpClientLock = NSLock()

let consoleTCPServeQueue = DispatchQueue(label: "virelaios.console-tcp.client")

/// Pre-auth deadline (ms): a client that never answers the challenge is dropped.
let consoleTCPAuthTimeoutMs = 10_000

/// Post-auth input bound: bytes without CR/LF before the client is dropped.
/// 4x the guest line editor's buffer (kernel/src/lineedit.zig `max_line` =
/// 256) — the guest refuses the excess anyway; this bounds the pump itself.
let consoleTCPLineMax = 1024

/// Output stall bound (s): a client whose socket send blocks this long is dropped.
let consoleTCPSendTimeoutSec = 2

func consoleTCPSay(_ msg: String) {
    print("  console-tcp: \(msg)")
    FileHandle.standardOutput.synchronizeFile()
}

/// Write every byte (EINTR-safe). False on error or SO_SNDTIMEO expiry.
func consoleTCPWriteAll(_ fd: Int32, _ bytes: [UInt8]) -> Bool {
    var off = 0
    while off < bytes.count {
        let w = bytes.withUnsafeBufferPointer { write(fd, $0.baseAddress! + off, bytes.count - off) }
        if w < 0 && errno == EINTR { continue }
        if w <= 0 { return false }
        off += w
    }
    return true
}

func consoleTCPEmit(_ data: Data) {
    // The whole write happens UNDER the lock: the serving loop clears
    // tcpClientFD under the same lock before it closes, so guest output can
    // never land on a recycled fd number that now belongs to a fresh (and
    // possibly still unauthenticated) client. The hold is bounded by
    // SO_SNDTIMEO (2 s), which is also the stall bound below.
    tcpClientLock.lock()
    defer { tcpClientLock.unlock() }
    let fd = tcpClientFD
    guard fd >= 0 else { return }
    if !consoleTCPWriteAll(fd, [UInt8](data)) {
        // The send timed out or the peer is gone: drop the client rather
        // than let it stall the guest-output tee. shutdown() wakes the
        // serving loop's read, which then closes the fd.
        tcpClientFD = -1
        shutdown(fd, SHUT_RDWR)
        consoleTCPSay("client stalled (output not drained within \(consoleTCPSendTimeoutSec) s; client dropped)")
    }
}

/// Read one byte before `deadline`: 1 = byte, 0 = EOF, -1 = error, -2 = timeout.
func consoleTCPReadByteTimed(_ fd: Int32, _ byte: inout UInt8, deadline: DispatchTime) -> Int {
    while true {
        let now = DispatchTime.now().uptimeNanoseconds
        guard deadline.uptimeNanoseconds > now else { return -2 }
        let remainMs = min((deadline.uptimeNanoseconds - now) / 1_000_000, UInt64(Int32.max))
        var pfd = pollfd(fd: fd, events: Int16(POLLIN), revents: 0)
        let pr = poll(&pfd, 1, Int32(remainMs))
        if pr < 0 { if errno == EINTR { continue }; return -1 }
        if pr == 0 { return -2 }
        let r = read(fd, &byte, 1)
        if r < 0 && errno == EINTR { continue }
        return r
    }
}

func consoleTCPEndServing() {
    tcpClientLock.lock(); tcpClientServing = false; tcpClientLock.unlock()
}

/// Serve one accepted client to completion (auth, pump, teardown).
func consoleTCPServe(_ c: Int32) {
    var tv = timeval(tv_sec: consoleTCPSendTimeoutSec, tv_usec: 0)
    _ = setsockopt(c, SOL_SOCKET, SO_SNDTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
    // Keepalive so a peer that vanished without a FIN (network drop, host
    // sleep) is detected: first probe after 15 s idle, then 3 x 5 s.
    var on: Int32 = 1
    _ = setsockopt(c, SOL_SOCKET, SO_KEEPALIVE, &on, socklen_t(MemoryLayout<Int32>.size))
    var kaIdle: Int32 = 15
    _ = setsockopt(c, IPPROTO_TCP, TCP_KEEPALIVE, &kaIdle, socklen_t(MemoryLayout<Int32>.size))
    var kaIntvl: Int32 = 5
    _ = setsockopt(c, IPPROTO_TCP, TCP_KEEPINTVL, &kaIntvl, socklen_t(MemoryLayout<Int32>.size))
    var kaCnt: Int32 = 3
    _ = setsockopt(c, IPPROTO_TCP, TCP_KEEPCNT, &kaCnt, socklen_t(MemoryLayout<Int32>.size))

    if let secret = consoleTCPSecret {
        // M50 TS4 (#1138, ADR 0024 D7): HMAC-SHA256 challenge-response. Send
        // a fresh 32-byte challenge line, read the client's one-line hex MAC
        // under the pre-auth deadline, and verify it in constant time. No
        // secret configured => byte-identical to the pre-TS4 bridge.
        var challenge = [UInt8](repeating: 0, count: 32)
        for i in 0..<challenge.count { challenge[i] = UInt8.random(in: 0...255) }
        let challengeLine = "VIRELAIOS-AUTH/1 hmac-sha256 " + hexString(challenge) + "\n"
        _ = consoleTCPWriteAll(c, Array(challengeLine.utf8))
        let deadline = DispatchTime.now() + .milliseconds(consoleTCPAuthTimeoutMs)
        var line = [UInt8]()
        var newline = false
        var timedOut = false
        var one: UInt8 = 0
        while line.count <= 160 {
            let r = consoleTCPReadByteTimed(c, &one, deadline: deadline)
            if r == -2 { timedOut = true; break }
            if r <= 0 { break }
            if one == 0x0a { newline = true; break }
            if one == 0x0d { continue }
            line.append(one)
        }
        if timedOut {
            _ = consoleTCPWriteAll(c, Array("console-tcp: auth timeout\n".utf8))
            close(c)
            consoleTCPEndServing()
            consoleTCPSay("auth timeout (no answer within \(consoleTCPAuthTimeoutMs / 1000) s; still listening)")
            return
        }
        var message = Data("VIRELAIOS-AUTH/1 hmac-sha256".utf8)
        message.append(0)
        message.append(contentsOf: challenge)
        let key = SymmetricKey(data: Data(secret.utf8))
        let mac = newline ? hexDecode(String(decoding: line, as: UTF8.self)) : nil
        let ok = mac.map { HMAC<SHA256>.isValidAuthenticationCode($0, authenticating: message, using: key) } ?? false
        guard ok else {
            _ = consoleTCPWriteAll(c, Array("console-tcp: auth failed\n".utf8))
            close(c)
            consoleTCPEndServing()
            consoleTCPSay("auth failed (MAC mismatch; still listening)")
            return
        }
        consoleTCPSay("client authenticated")
    }
    tcpClientLock.lock(); tcpClientFD = c; tcpClientLock.unlock()
    consoleTCPSay("client connected")
    var buf = [UInt8](repeating: 0, count: 4096)
    var lineLen = 0 // bytes of the guest's current line (since the last CR/LF)
    var dropReason: String?
    while true {
        var n: Int
        repeat { n = read(c, &buf, buf.count) } while n < 0 && errno == EINTR
        if n <= 0 { break }
        var overflow = false
        for i in 0..<n {
            if buf[i] == 0x0a || buf[i] == 0x0d {
                lineLen = 0
            } else {
                lineLen += 1
                if lineLen > consoleTCPLineMax { overflow = true; break }
            }
        }
        if overflow {
            // Nothing from the offending chunk reaches the guest.
            _ = consoleTCPWriteAll(c, Array("console-tcp: line too long\n".utf8))
            dropReason = "line too long (> \(consoleTCPLineMax) bytes without a newline; client dropped)"
            break
        }
        let data = Data(bytes: buf, count: n)
        do { try consoleInputPipe.fileHandleForWriting.write(contentsOf: data) } catch { break }
    }
    tcpClientLock.lock()
    if tcpClientFD == c { tcpClientFD = -1 }
    tcpClientLock.unlock()
    close(c)
    // Free the seat BEFORE the cancel below so the next client is admitted
    // as soon as the socket is gone; its bytes queue behind the cancel on
    // this serial queue, so ordering into the guest is preserved.
    consoleTCPEndServing()
    let midLine = lineLen > 0
    if midLine {
        // Leave no zombie seat state: cancel the partial line sitting in the
        // guest's editor so the next client starts at a clean prompt.
        try? consoleInputPipe.fileHandleForWriting.write(contentsOf: Data([0x03]))
    }
    if let dropReason { consoleTCPSay(dropReason) }
    consoleTCPSay("client disconnected" + (midLine ? " mid-line (partial line cancelled with ^C)" : "") + " (still listening)")
}

func startTCPConsoleBridge(port: UInt16, bindHost: String) {
    let listenQueue = DispatchQueue(label: "virelaios.console-tcp")
    listenQueue.async {
        let s = socket(AF_INET, SOCK_STREAM, 0)
        guard s >= 0 else {
            FileHandle.standardError.write(Data("WARNING: --console-tcp: socket() failed (errno=\(errno))\n".utf8))
            return
        }
        var reuse: Int32 = 1
        _ = setsockopt(s, SOL_SOCKET, SO_REUSEADDR, &reuse, socklen_t(MemoryLayout<Int32>.size))
        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = port.bigEndian
        if inet_pton(AF_INET, bindHost, &addr.sin_addr) != 1 {
            addr.sin_addr.s_addr = inet_addr("127.0.0.1")
        }
        let bindRC = withUnsafePointer(to: &addr) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(s, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
        }
        guard bindRC == 0 else {
            FileHandle.standardError.write(Data("WARNING: --console-tcp: bind \(bindHost):\(port) failed (errno=\(errno))\n".utf8))
            close(s)
            return
        }
        // Backlog > 1 so concurrent connectors are ACCEPTED and refused
        // explicitly (`busy`) instead of silently SYN-dropped.
        guard listen(s, 4) == 0 else {
            FileHandle.standardError.write(Data("WARNING: --console-tcp: listen failed (errno=\(errno))\n".utf8))
            close(s)
            return
        }
        consoleTCPSay("listening on \(bindHost):\(port) — connect with `nc \(bindHost) \(port)`")
        while true {
            var caddr = sockaddr()
            var clen = socklen_t(MemoryLayout<sockaddr>.size)
            let c = accept(s, &caddr, &clen)
            if c < 0 { if errno == EINTR { continue }; break }
            // A peer that closes while guest output is streaming makes the
            // tee's next write() raise SIGPIPE, which kills the whole runner
            // (exit 141) before the serving loop reads the EOF. Fail the write
            // with EPIPE instead, so the existing drop path handles it.
            var noSigPipe: Int32 = 1
            _ = setsockopt(c, SOL_SOCKET, SO_NOSIGPIPE, &noSigPipe, socklen_t(MemoryLayout<Int32>.size))
            tcpClientLock.lock()
            let busy = tcpClientServing
            if !busy { tcpClientServing = true }
            tcpClientLock.unlock()
            if busy {
                // ADR 0022 D2 (M70g amendment): one client at a time, and
                // the second one is TOLD so — no backlog limbo.
                _ = consoleTCPWriteAll(c, Array("console-tcp: busy\n".utf8))
                close(c)
                consoleTCPSay("second client refused (busy; one client at a time)")
                continue
            }
            consoleTCPServeQueue.async { consoleTCPServe(c) }
        }
        close(s)
    }
}
