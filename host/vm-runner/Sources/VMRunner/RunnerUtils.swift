// Shared helpers: fatal exits, hex codecs, entitlement and
// signature utilities used by several lanes.

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


func fail(_ message: String) -> Never {
    restoreTerminal()
    FileHandle.standardError.write(Data("ERROR: \(message)\n".utf8))
    if hostSessionMode {
        HostApplication.presentLaunchFailure(message)
    }
    exit(1)
}

func hostArchitecture() -> String {
    var u = utsname()
    uname(&u)
    return withUnsafeBytes(of: &u.machine) { raw -> String in
        var s = ""
        for byte in raw {
            if byte == 0 { break }
            s.append(String(UnicodeScalar(byte)))
        }
        return s
    }
}

// M113b (#2418, ADR 0054 D5): the PE/COFF Authenticode digest — the image
// hashed in file order skipping the CheckSum field (4 bytes), the
// IMAGE_DIRECTORY_ENTRY_SECURITY entry (8 bytes), and the Attribute
// Certificate Table it points to (a file offset, not an RVA). This is the
// convention the M113a probe observed the firmware db enforcing (ADR 0054
// Grounding 3: the raw file hash is refused, this digest boots).
func peAuthenticodeSHA256(_ data: Data) -> Data? {
    let b = [UInt8](data)
    func u16le(_ off: Int) -> Int { Int(b[off]) | (Int(b[off + 1]) << 8) }
    func u32le(_ off: Int) -> Int {
        Int(b[off]) | (Int(b[off + 1]) << 8) | (Int(b[off + 2]) << 16) | (Int(b[off + 3]) << 24)
    }
    guard b.count >= 0x40 else { return nil }
    let peOff = u32le(0x3c)
    guard peOff + 24 <= b.count,
          b[peOff] == 0x50, b[peOff + 1] == 0x45, b[peOff + 2] == 0, b[peOff + 3] == 0 else { return nil }
    let optOff = peOff + 4 + 20 // PE signature + COFF header
    let magic = u16le(optOff)
    let dirBase: Int
    let numDirsOff: Int
    switch magic {
    case 0x10b: dirBase = optOff + 96; numDirsOff = optOff + 92 // PE32
    case 0x20b: dirBase = optOff + 112; numDirsOff = optOff + 108 // PE32+
    default: return nil
    }
    guard numDirsOff + 4 <= b.count else { return nil }
    let numDirs = u32le(numDirsOff)
    let checksumOff = optOff + 64
    let secDirOff = dirBase + 4 * 8 // IMAGE_DIRECTORY_ENTRY_SECURITY
    var hasher = SHA256()
    if numDirs <= 4 || secDirOff + 8 > b.count {
        guard checksumOff + 4 <= b.count else { return nil }
        hasher.update(data: data[0..<checksumOff])
        hasher.update(data: data[(checksumOff + 4)..<b.count])
    } else {
        let certOff = u32le(secDirOff)
        let certSize = u32le(secDirOff + 4)
        guard checksumOff + 4 <= secDirOff else { return nil }
        hasher.update(data: data[0..<checksumOff])
        hasher.update(data: data[(checksumOff + 4)..<secDirOff])
        let contentEnd = certOff == 0 ? b.count : min(certOff, b.count)
        guard secDirOff + 8 <= contentEnd else { return nil }
        hasher.update(data: data[(secDirOff + 8)..<contentEnd])
        if certOff != 0 {
            guard certOff + certSize <= b.count else { return nil }
            hasher.update(data: data[(certOff + certSize)..<b.count])
        }
    }
    return Data(hasher.finalize())
}

/// Parse a 64-hex-char digest string into bytes (nil on bad input).
func dataFromHex(_ hex: String) -> Data? {
    let chars = Array(hex.utf8)
    guard chars.count % 2 == 0 else { return nil }
    var out = Data()
    out.reserveCapacity(chars.count / 2)
    var i = 0
    func nibble(_ c: UInt8) -> UInt8? {
        switch c {
        case UInt8(ascii: "0")...UInt8(ascii: "9"): return c - UInt8(ascii: "0")
        case UInt8(ascii: "a")...UInt8(ascii: "f"): return c - UInt8(ascii: "a") + 10
        case UInt8(ascii: "A")...UInt8(ascii: "F"): return c - UInt8(ascii: "A") + 10
        default: return nil
        }
    }
    while i + 1 < chars.count {
        guard let hi = nibble(chars[i]), let lo = nibble(chars[i + 1]) else { return nil }
        out.append(hi << 4 | lo)
        i += 2
    }
    return out
}

// M117d (issue #2476, ADR 0057 D11): the opt-in real-LAN attachments get
// the same one-network-device bound, then their own named refusals — the
// absent interface names itself, the absent com.apple.vm.networking
// entitlement names itself (checked in THIS signature: it is what VZ's
// config.validate() consults), and a non-success vmnet_return_t carries
// its status. Both keys are restricted entitlements an ad-hoc codesign
// cannot carry (observed 2026-10-17, macOS 27.2: amfid kills the process
// before main — "adhoc signed but contains restricted entitlements"), so
// they live in host/vm-runner/entitlements-net.plist for a
// development-signed build; entitlements.plist stays the ad-hoc-safe set.
func runnerHasEntitlement(_ name: String) -> Bool {
    guard let task = SecTaskCreateFromSelf(nil),
          let value = SecTaskCopyValueForEntitlement(task, name as CFString, nil) else {
        return false
    }
    return (value as? Bool) ?? (value as? NSNumber)?.boolValue ?? false
}

// M50 TS4 (#1138, ADR 0024 D6): the host-initiated client's challenge
// answer. Lower-case hex of a byte array; the inverse decode; and the
// `VIRELAIOS-AUTH/1 <scheme> <hex-challenge>` line parser/answer.
func hexString(_ bytes: [UInt8]) -> String {
    let digits = Array("0123456789abcdef")
    var out = ""
    out.reserveCapacity(bytes.count * 2)
    for b in bytes {
        out.append(digits[Int(b >> 4)])
        out.append(digits[Int(b & 0xf)])
    }
    return out
}

func hexDecode(_ s: String) -> [UInt8]? {
    let chars = Array(s.utf8)
    guard chars.count % 2 == 0 else { return nil }
    func nib(_ c: UInt8) -> UInt8? {
        switch c {
        case 0x30...0x39: return c - 0x30
        case 0x61...0x66: return c - 0x61 + 10
        case 0x41...0x46: return c - 0x41 + 10
        default: return nil
        }
    }
    var out = [UInt8]()
    out.reserveCapacity(chars.count / 2)
    var i = 0
    while i < chars.count {
        guard let hi = nib(chars[i]), let lo = nib(chars[i + 1]) else { return nil }
        out.append((hi << 4) | lo)
        i += 2
    }
    return out
}
