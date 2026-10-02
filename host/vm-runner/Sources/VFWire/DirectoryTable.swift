// B2: immutable, lossless directory snapshots for the legacy file channel.
// No Virtualization dependency; the real runner and class-A tests share this.
import Foundation
#if canImport(Darwin)
import Darwin
#else
import Glibc
#endif

public final class DirectoryTable {
    public static let nameMax = 255
    public static let depthMax = 8
    public static let entryMax = 256
    public static let pageMax = 16
    public static let rowBytes = 272
    private struct Snapshot {
        let token: UInt64
        let rows: [[UInt8]]
    }
    private var slots = [Snapshot?](repeating: nil, count: 8)
    private var nextToken: UInt64 = 1

    public init() {}

    public static func validPath(_ path: String) -> Bool {
        let components = path.split(separator: "/", omittingEmptySubsequences: false)
        return path.utf8.count <= VFWire.pathMax && !path.utf8.contains(0) &&
            (path.isEmpty || (components.count <= depthMax && components.allSatisfy {
                !$0.isEmpty && $0 != "." && $0 != ".." && $0.utf8.count <= nameMax
            }))
    }

    private static func stamp(_ url: URL) -> [Int64]? {
        var value = stat()
        guard lstat(url.path, &value) == 0 else { return nil }
        #if canImport(Darwin)
        return [Int64(bitPattern: UInt64(value.st_ino)), Int64(value.st_mtimespec.tv_sec), Int64(value.st_mtimespec.tv_nsec),
                Int64(value.st_ctimespec.tv_sec), Int64(value.st_ctimespec.tv_nsec)]
        #else
        return [Int64(bitPattern: UInt64(value.st_ino)), Int64(value.st_mtim.tv_sec), Int64(value.st_mtim.tv_nsec),
                Int64(value.st_ctim.tv_sec), Int64(value.st_ctim.tv_nsec)]
        #endif
    }

    private static func put(_ value: UInt64, _ bytes: inout [UInt8], at: Int, width: Int) {
        for i in 0..<width { bytes[at + i] = UInt8(truncatingIfNeeded: value >> (i * 8)) }
    }

    private static func get(_ bytes: [UInt8], at: Int, width: Int) -> UInt64 {
        var value: UInt64 = 0
        for i in 0..<width { value |= UInt64(bytes[at + i]) << (i * 8) }
        return value
    }

    public static func encodeRow(name: [UInt8], size: UInt64, isDirectory: Bool) -> [UInt8]? {
        guard !name.isEmpty, name.count <= nameMax, !name.contains(0), !name.contains(47),
              name != [46], name != [46, 46] else { return nil }
        var row = [UInt8](repeating: 0, count: rowBytes)
        row.replaceSubrange(0..<name.count, with: name)
        put(isDirectory ? 0 : size, &row, at: 256, width: 8)
        put(UInt64(name.count), &row, at: 264, width: 2)
        row[266] = isDirectory ? 1 : 0
        return row
    }

    /// Capture first, then publish a token. During-capture mutation refuses;
    /// after-capture mutation cannot alter any subsequent page.
    /// The hook is only for deterministic tests of the capture race.
    public func open(root: URL, path: String, duringCapture: (() throws -> Void)? = nil) -> (UInt8, [UInt8]) {
        guard Self.validPath(path) else { return (VFWire.stPathLimit, []) }
        guard let url = VFWire.resolveSubpath(root: root, path: path) else { return (VFWire.stHostError, []) }
        guard let slot = slots.firstIndex(where: { $0 == nil }), nextToken < UInt64.max else {
            return (VFWire.stHandle, [])
        }
        let fm = FileManager.default
        var isDirectory: ObjCBool = false
        guard fm.fileExists(atPath: url.path, isDirectory: &isDirectory) else { return (VFWire.stNotFound, []) }
        guard isDirectory.boolValue else { return (VFWire.stIsDir, []) }
        guard let before = Self.stamp(url) else { return (VFWire.stHostError, []) }
        do {
            let items = try fm.contentsOfDirectory(at: url, includingPropertiesForKeys: nil)
                .sorted { Array($0.lastPathComponent.utf8).lexicographicallyPrecedes(Array($1.lastPathComponent.utf8)) }
            guard items.count <= Self.entryMax else { return (VFWire.stLimit, []) }
            var rows: [[UInt8]] = []
            for item in items {
                let name = Array(item.lastPathComponent.utf8)
                guard !name.isEmpty, name.count <= Self.nameMax, !name.contains(0) else {
                    return (VFWire.stPathLimit, [])
                }
                let values = try fm.attributesOfItem(atPath: item.path)
                let type = values[.type] as? FileAttributeType
                guard let size = values[.size] as? NSNumber, type != nil else { return (VFWire.stHostError, []) }
                guard let row = Self.encodeRow(name: name, size: size.uint64Value, isDirectory: type == .typeDirectory) else {
                    return (VFWire.stPathLimit, [])
                }
                rows.append(row)
            }
            try duringCapture?()
            guard let after = Self.stamp(url), before == after else { return (VFWire.stChanged, []) }
            // Compare names too, so even timestamp-preserving changes refuse.
            let afterNames = try fm.contentsOfDirectory(atPath: url.path).map { Array($0.utf8) }
                .sorted { $0.lexicographicallyPrecedes($1) }
            guard afterNames == items.map({ Array($0.lastPathComponent.utf8) }) else { return (VFWire.stChanged, []) }
            let token = nextToken
            nextToken += 1
            slots[slot] = Snapshot(token: token, rows: rows)
            var reply = [UInt8](repeating: 0, count: 8)
            Self.put(token, &reply, at: 0, width: 8)
            return (VFWire.stOk, reply)
        } catch {
            return (VFWire.stHostError, [])
        }
    }

    public func page(_ payload: [UInt8]) -> (UInt8, [UInt8]) {
        guard payload.count == 12 else { return (VFWire.stHostError, []) }
        let token = Self.get(payload, at: 0, width: 8)
        guard let snapshot = slots.compactMap({ $0 }).first(where: { $0.token == token }) else {
            return (VFWire.stHandle, [])
        }
        let offset = Int(Self.get(payload, at: 8, width: 2))
        let limit = Int(Self.get(payload, at: 10, width: 2))
        guard offset <= snapshot.rows.count, (1...Self.pageMax).contains(limit) else { return (VFWire.stHostError, []) }
        let count = min(limit, snapshot.rows.count - offset)
        var reply = [UInt8](repeating: 0, count: 16)
        Self.put(UInt64(offset + count), &reply, at: 0, width: 8)
        Self.put(UInt64(count), &reply, at: 8, width: 4)
        Self.put(offset + count == snapshot.rows.count ? 1 : 0, &reply, at: 12, width: 4)
        for row in snapshot.rows[offset..<(offset + count)] { reply.append(contentsOf: row) }
        return (VFWire.stOk, reply)
    }

    public func close(_ payload: [UInt8]) -> (UInt8, [UInt8]) {
        guard payload.count == 8 else { return (VFWire.stHostError, []) }
        let token = Self.get(payload, at: 0, width: 8)
        guard let slot = slots.firstIndex(where: { $0?.token == token }) else { return (VFWire.stHandle, []) }
        slots[slot] = nil
        return (VFWire.stOk, [])
    }
}
