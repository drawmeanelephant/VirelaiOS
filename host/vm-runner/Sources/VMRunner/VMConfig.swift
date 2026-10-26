// Machine construction: EFI variable store, VZ configuration,
// the VM object, delegates, vsock listeners, and the command bus.

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


let varsURL = URL(fileURLWithPath: varsOverridePath ?? "artifacts/efi-vars.bin")

let bootLoader = VZEFIBootLoader()

let config = VZVirtualMachineConfiguration()

// M102d3 (#2168): the opt-in private render device, attached AFTER the
// existing custom-virtio attach — the leaf's attach() APPENDS to
// config.customVirtioDevices, so the 0x42 channel, the native 2D GPU and
// every other device array stay untouched. Assemble builds c2's real
// Metal backend first: a Metal init failure is a named stop here, never
// a fallback. If VZ later refuses the configuration, config.validate()
// below reports its exact error — do not substitute another device.
// gpu3dCountersReport stays nil without the flag (and on every non-SPIKE
// binary), so teardown reporting is compiled in everywhere but fires
// only when the device was actually attached.
var gpu3dCountersReport: (() -> String)?

#if SPIKE
// Held for the VM's lifetime: VZ holds the provider's delegates weakly,
// so dropping the attachment would make the device go deaf (the same
// retention discipline as CustomVirtioSpike's static delegates).
@available(macOS 27.0, *)
enum GPU3DRunnerRetain {
    static var attachment: GPU3DRunnerAttachment?
}
#endif

final class Runner: NSObject {
    let vm: VZVirtualMachine
    let queue = DispatchQueue(label: "virelaios.vm")
    init(configuration: VZVirtualMachineConfiguration) {
        vm = VZVirtualMachine(configuration: configuration, queue: queue)
        super.init()
    }
}

// ---------------------------------------------------------------------------
// VM stop reasons (claim #1278, the unblock for #1261).
//
// VZVirtualMachineDelegate exists precisely to carry the reason a VM stopped,
// and its own header says so (VZVirtualMachine.h: "For error cases, use the
// `state` property and information passed through the VZVirtualMachineDelegate
// to inspect the virtual machine's current state"). This binary set no
// delegate, so that information was discarded and a dying boot could only ever
// be reported as the bare state rawValue — which is why #1261 spent a whole
// investigation looking at `state=3` with no reason attached.
//
// Every member of the protocol is `@optional`, which makes a wrong signature
// compile silently and simply never be called. Both names below are therefore
// witnessed, not guessed: `#selector(VZVirtualMachineDelegate.guestDidStop(_:))`
// and `#selector(VZVirtualMachineDelegate.virtualMachine(_:didStopWithError:))`
// each resolve, which a wrong spelling could not.
//
// This class is only the VZ selector surface: the recording rules (what an
// error line looks like, and that an error outranks a racing clean stop) live in
// the VMPostmortem module, where `swift test` pins them without a VM.
final class VMRunnerDelegate: NSObject, VZVirtualMachineDelegate {
    let stopReasons = StopReasonRecorder()

    func guestDidStop(_ virtualMachine: VZVirtualMachine) {
        stopReasons.recordGuestPoweredOff()
    }

    func virtualMachine(_ virtualMachine: VZVirtualMachine, didStopWithError error: Error) {
        stopReasons.recordError(error)
    }

    /// The ` reason=…` suffix for a verdict line (never empty: a VM that stopped
    /// without VZ narrating it says so).
    func verdict() -> String { stopReasons.verdictSuffix() }
}

// M115a (#2460): the host-side accept half of the vsock probe. VZ calls the
// delegate on the VM's queue whenever the guest issues a connect to the
// listened port; each accepted connection is retained because the SDK
// documents that an accepted connection's fd dies with the object.
final class VsockListenerDelegate: NSObject, VZVirtioSocketListenerDelegate {
    private var connections: [VZVirtioSocketConnection] = []

    func listener(_ listener: VZVirtioSocketListener,
                  shouldAcceptNewConnection connection: VZVirtioSocketConnection,
                  from socketDevice: VZVirtioSocketDevice) -> Bool {
        connections.append(connection)
        print("VSOCK: listener accepted guest connect dport=\(connection.destinationPort) sport=\(connection.sourcePort) n=\(connections.count)")
        return true
    }
}

// Held strongly: `VZVirtioSocketListener.delegate` is weak, and the listener
// itself must outlive the VM's socket device for connects to keep landing.
let vsockListenerDelegate = VsockListenerDelegate()

let vsockListener = VZVirtioSocketListener()

// M115b (#2461): the ctl listener + its credential-bearing delegate are
// created at arm time (the key only exists once the share dir is known).
var vsockCtlDelegate: VSockCtlListenerDelegate?

let vsockCtlListener = VZVirtioSocketListener()

// Held strongly: `VZVirtualMachine.delegate` is a weak property, so a
// temporary would be deallocated immediately and never called.
let vmDelegate = VMRunnerDelegate()

let runner = Runner(configuration: config)

// M114a (#2454): the bus every UI action dispatches through — the session
// window's own instance when it exists, a windowless one otherwise, so
// --host-cmd-after dispatches verbs down the identical seam the menus use.
let commandBus = hostApplication?.commandBus ?? HostCommandBus()

// M139a (#2779): the straight-line boot IS session 1 — adopted here so
// the registry's dedup (`profile_in_use`), the `vm=1` receipts and the
// fleet drain at exit cover it exactly like a `new-window` session.
let primarySession = VMSessionRegistry.shared.adoptPrimary(
    commandBus: commandBus, runner: runner, delegate: vmDelegate,
    serialURL: serialURL, profile: activeProfile)
