// B4 (#1871): one host rename, with no copy/unlink fallback. This helper is
// shared by the live legacy file-channel handler and VZ-free tests.
import Foundation
#if canImport(Darwin)
import Darwin
#endif

extension VFWire {
    public enum RenameMode {
        case preserveExisting
        case replace
    }

    public static func rename(root: URL, payload: [UInt8], mode: RenameMode) -> UInt8 {
        guard let nul = payload.firstIndex(of: 0),
              let from = String(bytes: payload[..<nul], encoding: .utf8),
              let to = String(bytes: payload[(nul + 1)...], encoding: .utf8),
              buildRenamePayload(from: from, to: to) != nil,
              let fromURL = renamePath(root: root, path: from),
              let toURL = renamePath(root: root, path: to),
              fromURL.path != root.resolvingSymlinksInPath().path,
              toURL.path != root.resolvingSymlinksInPath().path else {
            return stHostError
        }
        #if canImport(Darwin)
        let rc = fromURL.path.withCString { source in
            toURL.path.withCString { destination in
                switch mode {
                case .replace:
                    return Darwin.rename(source, destination)
                case .preserveExisting:
                    return renamex_np(source, destination, UInt32(RENAME_EXCL))
                }
            }
        }
        if rc == 0 { return stOk }
        switch errno {
        case ENOENT: return stNotFound
        case EEXIST: return stExists
        case EISDIR: return stIsDir
        case EACCES, EPERM: return stAccess
        case ENOTSUP, ENOSYS: return stUnsupported
        default: return stHostError
        }
        #else
        // No platform-specific safe primitive: refuse before mutation.
        return stUnsupported
        #endif
    }

    private static func renamePath(root: URL, path: String) -> URL? {
        guard !path.hasPrefix("/") else { return nil }
        let components = path.split(separator: "/")
        guard let leaf = components.last, leaf != ".", leaf != "..",
              !components.contains(where: { $0 == ".." || $0.contains("\\") }),
              let parent = resolveSubpath(root: root, path: components.dropLast().joined(separator: "/")) else {
            return nil
        }
        // A rename acts on the leaf entry, not its symlink target. In
        // particular, NOREPLACE must refuse even a dangling destination link.
        return parent.appendingPathComponent(String(leaf))
    }
}
