// VirelaiOS VM runner for the Apple Virtualization.framework path.
//
// Usage: VMRunner <disk-image> [serial-log] [--screen <png>]
//        VMRunner --overlay-base <base.img> [serial-log] [flags...]
// (--overlay-base: macOS 27 DiskImageKit stacked image — read-only base +
//  throwaway ASIF overlay per run; positional <disk-image> is then ignored.
//  --vars <path>: per-run EFI variable store.)
//         [--secure-boot --secure-boot-pk <DER cert>
//          --secure-boot-db-efi <PE> | --secure-boot-db-hash <64-hex>]
//          (M113b #2418, ADR 0054 Part I: opt-in UEFI Secure Boot — a
//          FRESH per-run variable store, a custom Platform Key, and db
//          entries of PE/COFF Authenticode digests (the convention the
//          firmware enforces) or raw hashes. The firmware refuses an
//          unpinned BOOTAA64.EFI before any guest code runs. Both
//          db flags are repeatable. OFF by default: without the flag the
//          store and every device are byte-identical.)
//         [--vz-restore] (claim #1370: standalone headless save/restore probe —
//          real VZ pause/saveMachineStateTo/stop/restoreMachineStateFrom/resume;
//          a fresh post-restore serial query must recover a RAM-only clipboard
//          marker. Supports only --overlay-base/--vars/--serial/--cpus/--timeout;
//          needs --timeout in (0,600]. See docs/hardware-contract.md.)
//         [--data-disk <img>] (M100b, #2307, ADR 0050: a second virtio-blk
//          function — the guest's VLFS data disk; raw RW image created if
//          absent, blockDeviceIdentifier "virelai-vlfs", served through a
//          kernel block device + VZDiskBlockDeviceStorageDeviceAttachment
//          (issue #3112: the file must be the guest-coherent view even
//          across a hard vm.stop). Coexists with --overlay-base, which
//          covers the boot disk only.)
//          [--vz-restore-save <dir>] [--vz-restore-load <dir>] (M70g G3, #1459:
//          restore beyond same-process — one process boots, stores the marker,
//          saves state + the ASIF overlay + the marker into <dir> and exits;
//          a NEW process restores from <dir> instead of booting and must
//          recover the marker.) Device knobs --display/--input/--usb-msd/
//          --custom-virtio/--gpu3d-metal/--vsock are accepted under
//          --vz-restore so their save/restore verdict can be measured
//          (validateSaveRestoreSupport + the full cycle); a refusal is
//          printed as `VZ-RESTORE: validateSaveRestoreSupport REFUSED ...`
//          before the error exit.
//         [--vsock [--vsock-port <n>] [--vsock-ctl [--vsock-ctl-port <n>]]]
//          (M115a, #2460: attach Apple's
//          VZVirtioSocketDevice and arm a host listener on port <n>,
//          default 1208. A guest connect to that port is accepted and
//          printed as `VSOCK: listener accepted guest connect ...`.
//          M115b, #2461: --vsock-ctl arms the virelai-ctl/1 endpoint on
//          --vsock-ctl-port, default 1209 — the HMAC-challenged
//          suspend/snapshot wire, keyed by the share's vsock-ctl
//          SECRETS.TXT row (either host channel's share is read); no
//          row, no listener. M115d, #2463: `suspend` is armed — pause,
//          save into the profile's saved-state/, reply ok, stop, exit.)
//         [--profile <path>] (M115c, #2462: boot from a .virelaivm bundle —
//          a manifest-gated directory holding efi-vars.bin, the PERSISTENT
//          overlay.asif over config.json's base image, vlfs.img, share/,
//          machine-id.bin and saved-state/. An absent path is minted; an
//          existing one must carry a readable virelaivm/1 manifest or the
//          run refuses by name. Profile members are DEFAULTS: explicit
//          --vars/--overlay-base/--data-disk/--cvc-file/--cpus flags win.
//          --profile-init mints/validates the bundle and exits without
//          booting — tools/session.sh runs it before seeding the share.)
//         [--resume <path>] (M115d, #2463: boot the profile's saved-state/
//          instead of the boot image — the continuation of a ctl `suspend`.
//          Requires an existing bundle AND a written state.vzsave; both
//          refuse by name. Mutually exclusive with --vz-restore and
//          --profile-init; the ctl listener re-arms so the resumed
//          machine can suspend again.)
//         [--apply-update] [--rollback-update] [--update-key <64-hex>]
//          (M115f, #2465: verify the signed staged set in <share>/UPDATE/
//          and swap it in as the boot base image, retaining <base>.prev;
//          --rollback-update swaps it back. The trust pin comes from
//          --update-key or the profile's config.json updatePubkey. A
//          refused set fails the run by name; absent is "none staged".)
//         [--timeout <s|0>] (0 = run until Ctrl-C) [--expect <line>] [--terminal-marker <line>]
//         [--host-session] (tools/session.sh's bundled local front door;
//          close/Quit stop the VM, failures stay visible until dismissed)
//         [--cpus <n>] (claim 907: VCPU count, default 2 — the four-core
//          four-domain stress gate boots 4)
//         [--memory-mib <n>] (M129a #2610: guest RAM in MiB, 64...2048,
//          default 512 — wins over a profile's config.json memoryMiB)
//         [--console] [--console-tcp [host:]port[:secret]] [--console-tcp-secret-file <path>]
//          (M70g G2 #1459: secret from a file's first line, never argv)
//         [--debug-input] [--dump-marker <file>]
//         [--nvram-console <file>] [--script <file>]
//         [--script-after <text>] [--script-expect <text>]
//         [--script-expect-tail <s>] (claim 4912: hold the VM this long
//          after the expected transcript first appears, so the kernel's
//          async reap/report tail reaches the serial log before teardown;
//          default 1.5, 0 = stop on first match) [--custom-virtio]
//         [--cvc-echo] (claim 3141: the host-initiated custom-virtio push
//          echo — implies --custom-virtio and attaches a third queue)
//         [--via-virtio] (claim 9588, issue #523 item 3: inject keys through
//          the custom-virtio INPUT queue — queue 3 — instead of synthesizing
//          NSEvents into a view; implies --custom-virtio AND --cvc-echo,
//          attaching the full four-queue shape. HID-shaped 16-byte messages
//          per docs/hardware-contract.md. No window, no activation wall
//          (#151), no silent synthesized drop (#179).)
//         [--cvc-snap] (claim 0680, issue #523 item 3 capstone: attach the
//          FIFTH virtqueue — the framebuffer-snapshot channel. The guest
//          streams its composed scanout over queue 4 as tagged binary
//          messages; the runner reassembles them into a raw BGRX file.
//          Implies --via-virtio.)
//         [--cvc-file <host-dir>] (M34 HF1+HF2, issues #735/#736: attach
//          the SIXTH virtqueue — the host file channel. The guest userland
//          filesystem becomes a macOS folder served over queue 5 with plain
//          FileManager calls rooted at <host-dir> (wire format in
//          Sources/VFWire/VFWire.swift). The deepest flag implies the full
//          five-queue shape below it.)
//         [--host-clipboard] (M108b #2325, ADR 0051: the bidirectional
//          clipboard lane — signed VHI1 record files in <share>/HOSTINT/.
//          Guest→host: seat-emitted CLIP_OUT is mirrored to the
//          pasteboard; host→guest: changeCount polls emit CLIP_IN the
//          seat applies. Requires --cvc-file and a `host-int` row in the
//          share's SECRETS.TXT; OFF by default — no directory, no
//          records, no pasteboard read.)
//         [--host-pasteboard <name>] (retarget the clipboard lane to
//          NSPasteboard(name:) — mandatory for gates so a run never
//          clobbers .general; modifier on --host-clipboard)
//         [--host-clip-in <text>] [--host-clip-in-after <marker>]
//          (test hook: write <text> to the lane pasteboard once <marker>
//          appears — a real changeCount bump the ordinary path then
//          carries in. Default trigger: `gotabwm: hostint armed`.)
//         [--host-drop] (M108c #2326, ADR 0051 D4: the host→guest file-drop
//          lane — the display view registers .fileURL; dropped files land
//          in <share>/INBOX/ via temp+rename (≤64 MiB/file, ≤16/gesture)
//          and a signed DROP record announces each for the seat's toast.
//          Requires --cvc-file, the `host-int` SECRETS.TXT row, and a
//          display path; OFF by default — no INBOX/, no drag target, no
//          records.)
//         [--host-drop-file <path>] [--host-drop-after <marker>]
//          (test hook: feed <path> through the real drop handler once
//          <marker> appears — repeatable for multi-file gestures.
//          Default trigger: `gotabwm: hostint armed`.)
//         [--host-notify] (M108d #2328, ADR 0051 D5: seat toasts cross the
//          glass — each kind-12 toast becomes a signed NOTIFY_OUT record
//          the runner posts as a UNUserNotificationCenter banner. Banners
//          exist only under the `just session` bundle; the bare runner
//          consumes records and logs `host-notify: dropped
//          reason=no_bundle` per record instead. Requires --cvc-file and
//          the `host-int` SECRETS.TXT row; OFF by default — no records
//          polled, no center touched.)
//         [--host-outbox] (M127d #2602, ADR 0051 V2-D5: the guest→host
//          export lane — GOSH `export` / GOFILES "Export to Host" stage
//          <share>/OUTBOX/<name> + a REQ intent marker; the seat signs an
//          OUTBOX record and the runner verifies, then moves the payload
//          to --host-outbox-dir (default ~/Downloads). Caps: ≤64 MiB/file
//          at move time, ≤64 exports/epoch, ≤1 record per tick. Requires
//          --cvc-file and the `host-int` SECRETS.TXT row; OFF by default
//          — no OUTBOX/, no records.)
//         [--host-outbox-dir <path>] (modifier on --host-outbox: the
//          export landing. Default ~/Downloads — the flag is the gate
//          redirect, so a class-B run never touches a real Downloads.)
//         [--host-speech] (M186d #3181, ADR 0051 V2-D8: the guest→host
//          speech lane — GOSH `say` / GOA11Y -say stage <share>/SAY/
//          SAY-<hex> intent markers; the seat signs a SPEAK record and
//          the runner verifies, then hands the bounded UTF-8 utterance
//          to NSSpeechSynthesizer — queued, never blocking the VM.
//          Requires --cvc-file and the `host-int` SECRETS.TXT row; OFF
//          by default — no SAY/, no records.)
//         [--host-print] (M202c #3386, ADR 0051: the guest→host print
//          lane — the print verbs in GOEDIT/WEB/GOREADER stage
//          <share>/PRINT/<name>.pdf + a PREQ-<hex> intent marker; the
//          seat signs a PRINT record and the runner verifies. A bundled
//          session shows NSPrintPanel per job (the human's click is the
//          per-job consent — no path prints unconfirmed); a bare runner
//          takes the consent-free degrade: the PDF moves to
//          --host-print-dir (default ~/Downloads), receipt `saved`. Caps:
//          ≤4 MiB/job, ≤16 jobs/epoch, ≤1 record per tick. Requires
//          --cvc-file and the `host-int` SECRETS.TXT row; OFF by default
//          — no PRINT/, no records.)
//         [--host-print-dir <path>] (modifier on --host-print: the
//          degrade landing. Default ~/Downloads — the flag is the gate
//          redirect, so a class-B run never touches a real Downloads.)
//         [--host-cmd-after <marker>:<verb>] (M114a #2454, index #2447:
//          dispatch <verb> on the HostCommand bus — the same seam every
//          AppKit menu item uses — once <marker> appears in the serial
//          stream. Repeatable; <verb> is the suffix after the LAST colon
//          and unknown verbs are a parse-time error. Every dispatch emits
//          one `host-ui: cmd=<verb> state=<...>` receipt on stdout.
//          Requires --script or --host-session, where the marker scan
//          runs.)
//         [--host-capture-keys] (M114b #2455, index #2447: arm the machine
//          view's capturesSystemKeys at startup by dispatching the
//          capture-keys verb on the HostCommand bus once the
//          VZVirtualMachineView exists — the same host-ui: receipt as
//          ⌥⌘K / the toolbar pill / a --host-cmd-after trigger. Requires
//          a view: --screen/--display/--host-session.)
//         [--host-serial-send <line>] (M114d #2457, index #2447: arm the
//          inspector drawer's input line — the send-serial verb (a
//          --host-cmd-after trigger, or the drawer's Send) writes it into
//          the console pipe, the same path --script forwards through.
//          The dispatch receipts `disabled` without a guest RX path:
//          --script/--console/--host-session/--vz-restore provide one.)
//         [--cvc-console-file <path>] (claim 0680: structured console —
//          capture every queue-1 guest log line to <path>; also arms the
//          guest's console tee with a kind-3 control message once the pool
//          is ready. Requires --custom-virtio.)
//         [--snapshot-after <marker>] (claim 0680: fire one kind-4 snapshot
//          request when <marker> appears in the serial stream; repeatable,
//          each instance writes <base>-<n>.raw. Requires --screen (the GPU
//          owns the framebuffer). Implies --cvc-snap.)
//         [--snapshot-out <base>] (claim 0680: override the snapshot output
//          base path; default is the --screen base + "-snap".)
//         [--snapshot-drain-timeout <sec>] (issue 1788: hold the VM at
//          teardown, up to <sec>, while an armed kind-4 snapshot stream is
//          still in flight, so a stop cannot eat a frame the gate is
//          entitled to. Default 20; 0, negative, or unparsable = stop
//          immediately, the --script-expect-tail convention.)
//         [--sound] (milestone fifteen card A1, claim 6140: attach one
//          VZVirtioSoundDeviceConfiguration with one
//          VZVirtioSoundDeviceOutputStreamConfiguration (the PCM output
//          stream) carrying a VZHostAudioOutputStreamSink, so the guest's
//          virtio-snd device is discovered and its output can play on the
//          host speakers. OFF by default: without the flag
//          config.soundDevices stays [] — every existing gate is
//          byte-identical.)
//         [--balloon] (M183b #3172: attach ONE
//          VZVirtioTraditionalMemoryBalloonDeviceConfiguration — the
//          traditional virtio-balloon device (public VZ API since
//          macOS 11). The guest's virtio_balloon driver inflates/deflates
//          toward the device's num_pages target on its polled idle pass.
//          OFF by default: without the flag config.memoryBalloonDevices
//          stays [] — every existing gate is byte-identical.)
//         [--balloon-target-after <marker>:<MiB>] (M183b: write <MiB> MiB
//          to the device's targetVirtualMachineMemorySize once <marker>
//          appears in the serial stream — a value below memorySize asks
//          the guest to inflate, restoring memorySize asks it to deflate.
//          Repeatable, fires once each; the marker scan runs on the
//          scripted poll like --host-cmd-after. Implies --balloon.)
//         [--script2 <file> --script2-after <text>] (claim 4613: a second
//          scripted phase, forwarded once after its own serial marker)
//         [--script3 <file> --script3-after <text>] (claim 7786: a third
//          scripted phase, forwarded once after its own serial marker)
//         [--net <capture-file>] (milestone five card N1, claim 1373: attach
//          one VZVirtioNetworkDeviceConfiguration with a
//          VZFileHandleNetworkDeviceAttachment so the guest's virtio-net TX
//          frames are captured byte-exactly to <capture-file> (raw Ethernet
//          bytes, host writes them as they arrive). The guest MAC is FIXED
//          (02:00:00:00:00:01) so the guest-side VIRTIO_NET_F_MAC read is
//          deterministic and gate-assertable. OFF by default: without the
//          flag config.networkDevices stays [] — every existing gate is
//          byte-identical.)
//         [--net-sntp-respond <host-ip>[:<port>]] (M83b #1775: a tiny
//          deterministic host-side SNTP server inside the capture thread
//          — the guest's client-mode (3) NTP request for <host-ip>:<port>
//          (default 123) is answered with an RFC 4330 server packet
//          carrying host wall-clock time, the request's transmit
//          timestamp echoed as the originate timestamp. Requires --net.
//          OFF by default.)
//         [--net-sntp-skew <seconds>] (M83b: signed seconds added to the
//          host clock for every served timestamp, so a gate can prove the
//          guest clock really moved; default 0; requires
//          --net-sntp-respond.)
//         [--net-ra-respond <v6-prefix>/64] [--net-ndp-respond <v6-addr>]
//          [--net-icmp6-respond <v6-addr>] (M117b #2474, ADR 0057 D10: the
//          three v6 hermetic responders — request-driven synthesizers in
//          the capture thread, the --net-arp-respond shape. On a guest
//          Router Solicitation, --net-ra-respond answers with an RA from
//          the router link-local fe80::ff:fe00:2 carrying the argument
//          /64 prefix as an A+L PIO. On a guest Neighbor Solicitation
//          whose target is a listed address, --net-ndp-respond (repeatable)
//          answers with an NA (S=1 O=1, TLLA 02:00:00:00:00:02); when
//          --net-ra-respond is set the router link-local is implicitly
//          listed. On an ICMPv6 echo request for the address,
//          --net-icmp6-respond answers with the echo reply — id/seq/
//          payload byte-exact. All require --net. An unlisted target or
//          an unset flag goes unanswered — the refusal leg.)
//         [--net-udp6-respond [<v6-addr>]:<host-port>]
//          [--net-tcp6-respond [<v6-addr>]:<host-port>] (M117c #2475,
//          ADR 0057: the v6 transport responders — the N5 UDP echo and
//          the N10 TCP handshake/echo shapes on the fixed 40-byte
//          header. The bracketed literal keeps the address's own colons
//          out of the port split — the same shape `vi.Dial` speaks.
//          The UDP6 reply echoes the datagram byte-exact (v6 src/dst
//          swapped, UDP ports swapped, the pseudo-header checksum over
//          the 16-byte addresses recomputed). The TCP6 server answers
//          the guest's SYN with a SYN-ACK (the FIXED ISN 0x12345678),
//          data with an ACK + the payload echoed (or the HTTP 200 on a
//          GET / port 80/8080 — the v4 responder's same answer table),
//          and FIN with FIN-ACK; per-connection state keys on the
//          guest's v6 src addr:port. Both require --net.)
//         [--net-dhcp-respond <lease-ip>] (milestone five card N8, claim
//          0351: a tiny deterministic host-side DHCP server inside the
//          capture thread — the guest's DHCPDISCOVER is answered with an
//          OFFER and its REQUEST with an ACK, both carrying the FIXED
//          gate-assertable lease {ip=<lease-ip>, mask 255.255.255.0,
//          gateway 10.0.0.1, server id=<lease-ip>, lease 3600}, the
//          guest's xid echoed byte-exact, written into the SAME
//          attachment socket end (VZ reads fds[0], so the guest receives
//          it). Requires --net. OFF by default: the default VM is
//          unchanged.)
//         [--net-tcp-respond <host-ip>:<host-port>] (milestone five card
//          N10, claim 7026: a tiny deterministic host-side TCP server
//          inside the capture thread — the guest's SYN is answered with a
//          SYN-ACK (the FIXED gate-assertable server ISN 0x12345678, ack
//          = the guest's ISN+1), its data segment with an ACK + the SAME
//          payload echoed byte-exact, and its FIN with a FIN-ACK, all
//          written into the SAME attachment socket end. An optional
//          `:handshake` suffix (card N11, claim 5357) answers the SYN
//          with a SYN-ACK then goes SILENT on data/FIN — the
//          deterministic black hole for the retransmission-bound run.
//          M51 SSH1 (#1168) added a `:packet` suffix (paced payload) and
//          M51 SSH5 (#1172) a `:ssh` suffix (the runner-hosted minimal
//          SSH-2 server, VSSH). M51 real-OpenSSH interop (#1209, goal
//          #1066, ADR 0025 D8) adds a `:relay` suffix with
//          [--net-tcp-respond-relay <host-ip>:<host-port>]: the same
//          deterministic TCP endpoint, but the byte stream is proxied
//          byte-for-byte (NO crypto in the runner) to that real upstream
//          TCP server — e.g. a local OpenSSH `sshd` — so the guest's
//          SSH.BIN negotiates with real OpenSSH. The documented manual
//          check, never a CI gate.
//          Requires --net. OFF by default: the default VM is unchanged.)
//         [--net-tcp-connect <guest-ip>:<port>[:<payload-file>|ssh]]
//          [--net-tcp-connect-stream <executable>] (hermetic --net gate only;
//           its exit closes the connection: status 0 sends FIN, else RST)
//          [--net-tcp-connect-stream-arg <arg>] (repeatable; stream argv)
//          [--net-tcp-connect-intruder] (stream only: a second SYN from
//           another port once the guest speaks; reports whether it answered)
//          [--net-tcp-connect-resyn <n>] (non-stream/non-ssh clients re-SYN
//           on each NEW after-marker sighting, n extra SYNs max — sequential
//           sessions paced by the server's own relisten marker)
//          [--net-tcp-connect-after-skip <n>] (applies to the last declared
//           client: its SYN ignores the first n after-marker sightings)
//          [--net-tcp-connect-after <text>]
//          [--net-tcp-connect-close-after <text>]
//          [--net-tcp-connect-secret <s>] (M50 TS4: answer the guest's
//           VIRELAIOS-AUTH/1 challenge with HMAC-SHA256(s))
//          [--net-tcp-connect-mac <hex>] (M50 TS4 replay: send this
//           pre-recorded MAC line instead of computing one)
//          [--net-tcp-connect-ssh-exec <cmd>] [--net-tcp-connect-ssh-user <name>]
//          [--net-tcp-connect-ssh-pty <cols>x<rows>]
//          [--net-tcp-connect-ssh-keys <file>]
//          [--net-tcp-connect-ssh-session-file <path>]
//          (M45 SH7, issue #1083,
//          ADR 0020 Amendment B: the host INITIATES an inbound TCP
//          connection TO a guest listener — the reverse of
//          --net-tcp-respond. After <text> (default "sh: remote") the host
//          sends a SYN, completes the handshake against the guest server's
//          fixed ISN 0x54321098, sends the payload file's bytes, captures
//          the guest shell's reply, and closes with a FIN — the guest
//          auto-detaches on disconnect. <close-after> (optional) delays the
//          FIN until the reply contains that text. M70g G1 (#1491): a
//          `:ssh` suffix speaks the ADR 0025 D2 profile as a client
//          (VSSH.SSHClient) against the in-guest GOSSHD.ELF, paced one
//          ≤192-byte segment per guest ACK. Requires --net. OFF by
//          default: every existing gate is byte-identical. M168f (#2962):
//          --net-tcp-connect-ssh-pty switches the channel request to
//          pty-req (TERM=xterm-256color) + shell; --net-tcp-connect-ssh-keys
//          is a line script (`after <text>` / `send <escaped>` /
//          `resize <c>x<r>`) evaluated against the decrypted stream; the
//          session bytes are appended to --net-tcp-connect-ssh-session-file.)
//         [--net-nat] (milestone five card N7, claim 4678: attach one
//          VZVirtioNetworkDeviceConfiguration with a
//          VZNATNetworkDeviceAttachment instead of the file-handle
//          attachment — the host serves as router + NAT for the guest's
//          accesses to outside networks. No capture file (the host
//          translates the frames — that is the point), so the N7 gate
//          asserts GUEST-OBSERVED COUNTERS, not capture bytes.
//          Mutually exclusive with --net (one network device per guest for
//          now); the fixed locally-administered MAC 02:00:00:00:00:01 is
//          set on the device config and what the guest actually observes
//          under NAT is a claim-time observation, pinned in the hardware
//          contract. OFF by default: without the flag
//          config.networkDevices stays [] — every existing gate is
//          byte-identical.)
//         [--net-bridged <iface>] (M117d, issue #2476, ADR 0057 D11:
//          attach one VZVirtioNetworkDeviceConfiguration with a
//          VZBridgedNetworkDeviceAttachment on the named
//          VZBridgedNetworkInterface — the guest sits on the host's real
//          LAN (its own MAC, a real DHCP lease). OFF by default. Every
//          missing piece refuses by name: an <iface> absent from
//          VZBridgedNetworkInterface.networkInterfaces names itself; the
//          com.apple.vm.networking entitlement absent from this binary's
//          signature names itself (it is a RESTRICTED entitlement — an
//          ad-hoc codesign cannot carry it; entitlements-net.plist is the
//          set for a development-signed build). Mutually exclusive with
//          --net/--net-nat/--net-vmnet — one network device per guest.)
//         [--net-vmnet] (M117d, issue #2476, ADR 0057 D11: attach one
//          VZVirtioNetworkDeviceConfiguration with a
//          VZVmnetNetworkDeviceAttachment on a shared-mode vmnet logical
//          network (vmnet_network_create, macOS 26+ — the daemon serves
//          DHCP + NAT44/66 by default). OFF by default. A non-success
//          vmnet_return_t from vmnet_network_create fails carrying the
//          status and naming the com.apple.developer.networking.vmnet
//          entitlement (or root) the daemon demands — also restricted,
//          also in entitlements-net.plist. Same mutual exclusivity.
//          [--net-vmnet-subnet <a.b.c.d>/<len>] pins the network's IPv4
//          subnet (e.g. 192.168.77.0/24 — the daemon picks a rotating
//          /24 under 192.168/16 otherwise). The address must be the
//          network base. Requires --net-vmnet.)
//         [--net-inject <file>] [--net-inject-after <text>] (milestone five
//          card N2, claim 6076: the host->guest RX direction of the SAME
//          --net attachment — the file's bytes are written into the socket
//          end VZ delivers from, exactly ONCE, when <text> appears in the
//          serial log (default "net: rx-armed" — the guest's queue-0 RX
//          buffer is guaranteed supplied by then; deterministic, not a
//          sleep). Requires --net. OFF by default: the default VM — and the
//          29-gate aggregate — stays byte-identical. The injected bytes are
//          the KNOWN raw Ethernet frame the guest's net recv must print
//          byte-exact; the guest's netsend echo proves the round trip.)
//         [--mac <addr>] (M206a, issue #3813, ADR 0057: replace the pinned
//          guest MAC 02:00:00:00:00:01 on ANY net attachment — the guest's
//          VIRTIO_NET_F_MAC read reports it, so two guests on one segment
//          can carry distinct addresses with no guest-side change. Locally
//          administered unicast only: a multicast or universally
//          administered address refuses by name. Requires a network
//          attachment. Default unchanged.)
//         [--net-hub <path>] (M206a, issue #3813, ADR 0057: relay each
//          guest TX datagram to a host-side L2 learning switch on the
//          UNIX datagram socket <path>, and write the hub's deliveries
//          back into the same attachment socketpair. The first runner to
//          bind <path> owns the hub in-process (learn src->port, unicast
//          forward, broadcast + multicast flood — 01:00:5e:* and
//          33:33:* ride dst[0] bit0); later runners bind <path>.cli-<pid>
//          as clients. A stale socket file is named by connect()
//          refusing, then unlinked and retaken. The hub owns DHCP on the
//          segment — one lease per source MAC out of 10.0.0.16..31 — so
//          --net-dhcp-respond refuses by name under it. Requires --net;
//          the capture stays byte-exact. Mutually exclusive with
//          --net-nat/--net-bridged/--net-vmnet.)
//
// * --custom-virtio (macOS 27 spike, audit step 3): attaches one
//   default-off VZCustomVirtioDeviceConfiguration so the guest's PCI
//   discovery can observe it on a real VZ boot (VID 0x1af4, DID 0x1082,
//   class 0x00/0x00, 2 queues). Without the flag the VM configuration is
//   byte-identical to before — all existing gates are unchanged. NOTE:
//   Xcode 27 beta 4 exposes no host-triggered guest-interrupt API; the
//   spike proves discovery + queue transport (audit step 4) and the
//   used-ring IRQ via returnToQueue (audit step 5, claim 0828).
//
// * --gpu3d-metal (M102d3, issue #2168): attaches ONE additional custom-
//   virtio device — the experimental private render device (virtio deviceID
//   0x43, PCI DID 0x1083, class 0x03/0x00, 2 queues) served by b2's VIR1
//   envelope core with c2's real Metal backend. APPENDED to the
//   custom-virtio list: the 0x42 channel and the native 2D GPU are
//   untouched, and without the flag the VM configuration is byte-identical
//   to before. A Metal device that cannot initialize is a named failure,
//   never a fallback. Requires the SPIKE build (macOS 27 custom-virtio SDK
//   types); an ordinary build rejects the flag by name.
//   [--gpu3d-test-mode <delay|wrong-fence|stall>] selects one of c2's
//   deterministic completion-fault knobs; SPIKE-only, requires
//   --gpu3d-metal, and OFF by default — no fault mode exists without it.
//
// * --virtio-fs <host-dir> (M83g): attach Apple's standard VirtioFS device,
//   independently of the custom-virtio spike. The directory is shared
//   writable under the guest's fixed `virelaios` tag. Unlike custom virtio,
//   this standard device is eligible for VZ machine-state save/restore.
//
// * --nvram-console <file> (M1.5 VZ serial-gate successor, claim 0015):
//   before exiting, reconstruct the kernel's post-exit console stream from
//   the EFI variable store (artifacts/efi-vars.bin). In nvram-console
//   builds the kernel writes every console byte as chunked EFI variables
//   `VirelaiC0`, `VirelaiC1`, ... via runtime SetVariable — the one
//   post-exit-safe device channel on VZ (post-exit access to the
//   virtio-pci transport hangs, claim 0013). Each chunk value is prefixed
//   with the in-band marker `VIRELAIC <idx>:`; the host byte-scans the
//   store (file order == write order), validates the chunk indices, and
//   concatenates the payloads. The reconstructed text is written to
//   <file> and printed. In this mode the exit code is 0 iff reconstructed
//   output is non-empty — the NVRAM channel, not the (silent) serial
//   channel, is the gate. The serial evidence gate above is unchanged
//   when the flag is absent.
//
//   NOTE: the reconstructed bytes travelled the NVRAM variable channel,
//   NOT the virtio serial pipe — vm-serial.log stays 0 B. This is the
//   fallback channel claim 0013 named, and it makes post-exit console
//   evidence observable on VZ for the first time.
// * --dump-marker <file> (ADR 0004 D4 fixed-memory-marker fallback, gate
//   work item 3, claim 0009): before exiting, read the EFI variable store
//   (artifacts/efi-vars.bin) and save the ordered ladder of M2_* marker
//   instances the kernel wrote (the kernel persists each takeover stage as
//   the non-volatile variable `VirelaiM2` via runtime SetVariable, which
//   survives ExitBootServices on VZ). In this mode the exit code is 0 iff at
//   least one marker instance was found — the marker channel, not the
//   (silent) serial channel, is the gate. The serial evidence gate above is
//   unchanged when the flag is absent.
//
//   NOTE: the original memory-dump form (scanning this process's address
//   space for the BSS marker, on the assumption that the in-process VZ guest
//   RAM is host-mapped) is provably impossible on VZ: a full submap-aware
//   walk finds no 256 MiB guest-RAM region and every M2_* hit is this
//   runner's own constant array (claim 0009, artifacts/marker-dump.txt). The
//   NVRAM ladder is the working form of the fallback.
//
// Two modes:
//   * default (evidence gate): starts the VZ guest, captures the
//     virtio-console serial stream to the log, and reports success only
//     after the requested serial line and terminal line have both been
//     observed. This is the milestone-two evidence path (`zig build run`)
//     and is unchanged by the M1.5 console work.
//   * --console (M1.5 host plumbing): duplex serial attachment. The
//     attachment's fileHandleForReading is a real host input source (stdin
//     forwarded through a pipe), guest output is teed live to the terminal
//     AND to the serial log, and the host terminal is put into character
//     mode with its original settings restored on normal exit, on
//     ^C / SIGTERM / SIGHUP, and on failure.
//
// Honest limits (see docs/status.md, ADR 0004):
//   * The guest serial console has NO RX path. Bytes forwarded to the
//     serial attachment enter the guest's virtio-console input device, but
//     nothing in this slice proves the kernel received them — guest RX is a
//     separate milestone slice. `--debug-input` only shows bytes handed to
//     the attachment, not guest receipt.
//   * The VZ serial gate is blocked as of 2026-08-06 (vm-serial.log empty in
//     every saved run), so console mode currently shows no guest output.
// No guest filesystem or POSIX dependency is added; the guest is untouched.

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

// Diagnostics and the console tee must survive signal exits (SIGINT/SIGTERM),
// so stdout is unbuffered: print() reaches the terminal/file immediately.
setbuf(stdout, nil)


while idx < arguments.count {
    let arg = arguments[idx]
    if vzRestore, arg.hasPrefix("--") {
        // M70g G3 (#1459): the probe also takes the save/load directory and
        // the device knobs whose save/restore support it measures.
        // ADR 0059 (M131b #2639): --vz-restore-settle names the base boot's
        // quiescent marker (save side); the --script* family arms a scripted
        // warm leg on the --vz-restore-load side (D2's allow-list).
        let valued = ["--overlay-base", "--vars", "--serial", "--cpus", "--timeout",
                      "--vz-restore-save", "--vz-restore-load", "--vz-restore-witness",
                      "--vz-restore-verify-command", "--vz-restore-verify-output",
                      "--vz-restore-settle",
                      "--script", "--script-after", "--script-expect", "--script-expect-tail",
                      "--script2", "--script2-after", "--script2-delay",
                      "--script3", "--script3-after", "--script3-delay",
                      "--usb-msd", "--virtio-fs", "--vsock-port", "--vsock-ctl-port"]
        let bare = ["--vz-restore", "--display", "--input", "--custom-virtio", "--gpu3d-metal", "--vsock", "--vsock-ctl"]
        guard valued.contains(arg) || bare.contains(arg) else {
            fail("--vz-restore is a standalone headless save/restore probe; supports only --overlay-base, --vars, --serial, --cpus, --timeout, --vz-restore-save/--vz-restore-load <dir>, --vz-restore-witness <serial-line>, --vz-restore-verify-command/--vz-restore-verify-output, --vz-restore-settle <marker>, the --script* family on --vz-restore-load, --virtio-fs <host-dir>, and the device knobs --display, --input, --usb-msd, --custom-virtio, --gpu3d-metal, --vsock.")
        }
        if valued.contains(arg) {
            guard idx + 1 < arguments.count, !arguments[idx + 1].hasPrefix("--") else {
                fail("--vz-restore: \(arg) requires a value.")
            }
            if arg == "--timeout", Double(arguments[idx + 1]) == nil {
                fail("--vz-restore: --timeout requires a numeric value.")
            }
        }
    }
    if arg == "--overlay-base", idx + 1 < arguments.count {
        overlayBasePath = arguments[idx + 1]
        idx += 2
    } else if arg == "--vars", idx + 1 < arguments.count {
        varsOverridePath = arguments[idx + 1]
        idx += 2
    } else if arg == "--serial", idx + 1 < arguments.count {
        // Per-run serial log path (#523 item 2): the default
        // artifacts/vm-serial.log is shared state two concurrent gates
        // would clobber.
        serialLogPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--screen", idx + 1 < arguments.count {
        screenshotPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--screenshot-after", idx + 1 < arguments.count {
        screenshotAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--screenshot-after-delay", idx + 1 < arguments.count {
        guard let s = Double(arguments[idx + 1]), s >= 0 else {
            fail("--screenshot-after-delay requires a non-negative seconds value, got '\(arguments[idx + 1])'.")
        }
        screenshotAfterDelay = s
        idx += 2
    } else if arg == "--host-write", idx + 1 < arguments.count {
        hostWritePath = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-write-content", idx + 1 < arguments.count {
        hostWriteContent = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-write-after", idx + 1 < arguments.count {
        hostWriteAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--display" {
        displayMode = true
        idx += 1
    } else if arg == "--input" {
        inputMode = true
        idx += 1
    } else if arg == "--host-session" {
        idx += 1
    } else if arg == "--sound" {
        soundMode = true
        idx += 1
    } else if arg == "--balloon" {
        // M183b (#3172): attach the traditional memory balloon device.
        balloonEnabled = true
        idx += 1
    } else if arg == "--balloon-target-after", idx + 1 < arguments.count {
        // M183b: <marker>:<MiB> — the target is the suffix after the
        // LAST colon (serial markers may carry colons of their own —
        // the --host-write-after precedent). An unparsable spec is a
        // parse-time error, not a trigger that silently never fires.
        let spec = arguments[idx + 1]
        guard let colon = spec.lastIndex(of: ":"),
              let mib = UInt64(spec[spec.index(after: colon)...]),
              !spec[..<colon].isEmpty else {
            fail("--balloon-target-after expects <marker>:<MiB> (e.g. 'm183b-pre:448').")
        }
        balloonTargetTriggers.append(BalloonTargetTrigger(
            marker: String(spec[..<colon]), targetMiB: mib))
        balloonEnabled = true
        idx += 2
    } else if arg == "--no-entropy" {
        // M97g (#2084): detach the virtio-entropy device so a gate can
        // prove slot 72 fails closed while the CSPRNG is unseeded.
        noEntropy = true
        idx += 1
    } else if arg == "--usb-msd", idx + 1 < arguments.count {
        usbMsdPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--data-disk", idx + 1 < arguments.count {
        dataDiskPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--profile", idx + 1 < arguments.count {
        profilePath = arguments[idx + 1]
        idx += 2
    } else if arg == "--resume", idx + 1 < arguments.count {
        resumeFromPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--profile-init" {
        profileInitOnly = true
        idx += 1
    } else if arg == "--apply-update" {
        applyUpdate = true
        idx += 1
    } else if arg == "--rollback-update" {
        rollbackUpdate = true
        idx += 1
    } else if arg == "--update-key", idx + 1 < arguments.count {
        updateKeyHex = arguments[idx + 1]
        idx += 2
    } else if arg == "--input-key", idx + 1 < arguments.count {
        guard let kc = UInt16(arguments[idx + 1]) else {
            fail("--input-key requires a numeric macOS virtual keycode, got '\(arguments[idx + 1])'.")
        }
        inputKeyCode = kc
        idx += 2
    } else if arg == "--input-key-after", idx + 1 < arguments.count {
        inputKeyAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--input-string", idx + 1 < arguments.count {
        inputString = arguments[idx + 1]
        idx += 2
    } else if arg == "--input-string-after", idx + 1 < arguments.count {
        inputStringAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--input-chords", idx + 1 < arguments.count {
        inputChords = arguments[idx + 1]
        idx += 2
    } else if arg == "--input-chords-after", idx + 1 < arguments.count {
        inputChordsAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--input-chords2", idx + 1 < arguments.count {
        inputChords2 = arguments[idx + 1]
        idx += 2
    } else if arg == "--input-chords2-after", idx + 1 < arguments.count {
        inputChords2After = arguments[idx + 1]
        idx += 2
    } else if arg == "--input-chords-delay", idx + 1 < arguments.count {
        guard let d = Double(arguments[idx + 1]), d > 0 else {
            fail("--input-chords-delay requires a positive seconds value, got '\(arguments[idx + 1])'.")
        }
        inputChordsDelay = d
        idx += 2
    } else if arg == "--chords-view" {
        // M34 HF5 (issue #739): see the chordsViaView declaration above.
        chordsViaView = true
        idx += 1
    } else if arg == "--pointer", idx + 1 < arguments.count {
        pointerScript = arguments[idx + 1]
        idx += 2
    } else if arg == "--pointer-after", idx + 1 < arguments.count {
        pointerAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--pointer-route", idx + 1 < arguments.count {
        pointerRoute = arguments[idx + 1]
        idx += 2
    } else if arg == "--pointer-request-trust" {
        pointerRequestTrust = true
        idx += 1
    } else if arg == "--vz-restore" {
        idx += 1
    } else if arg == "--vz-restore-save", idx + 1 < arguments.count {
        vzRestoreSaveDir = arguments[idx + 1]
        idx += 2
    } else if arg == "--vz-restore-load", idx + 1 < arguments.count {
        vzRestoreLoadDir = arguments[idx + 1]
        idx += 2
    } else if arg == "--vz-restore-settle", idx + 1 < arguments.count {
        vzRestoreSettle = arguments[idx + 1]
        idx += 2
    } else if arg == "--vz-restore-witness", idx + 1 < arguments.count {
        vzRestoreWitness = arguments[idx + 1]
        idx += 2
    } else if arg == "--vz-restore-verify-command", idx + 1 < arguments.count {
        vzRestoreVerifyCommand = arguments[idx + 1]
        idx += 2
    } else if arg == "--vz-restore-verify-output", idx + 1 < arguments.count {
        vzRestoreVerifyOutput = arguments[idx + 1]
        idx += 2
    } else if arg == "--secure-boot" {
        secureBootEnabled = true
        idx += 1
    } else if arg == "--secure-boot-pk", idx + 1 < arguments.count {
        secureBootPKPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--secure-boot-db-efi", idx + 1 < arguments.count {
        secureBootDbEfiPaths.append(arguments[idx + 1])
        idx += 2
    } else if arg == "--secure-boot-db-hash", idx + 1 < arguments.count {
        secureBootDbHashes.append(arguments[idx + 1])
        idx += 2
    } else if arg == "--virtio-fs", idx + 1 < arguments.count {
        virtioFSShareDir = arguments[idx + 1]
        idx += 2
    } else if arg == "--vsock" {
        vsockEnabled = true
        idx += 1
    } else if arg == "--vsock-port", idx + 1 < arguments.count {
        guard let port = UInt32(arguments[idx + 1]), port != 0 else {
            fail("--vsock-port requires a numeric port value in 1...4294967294.")
        }
        vsockPort = port
        vsockPortSet = true
        idx += 2
    } else if arg == "--vsock-ctl" {
        vsockCtlEnabled = true
        idx += 1
    } else if arg == "--vsock-ctl-port", idx + 1 < arguments.count {
        guard let port = UInt32(arguments[idx + 1]), port != 0 else {
            fail("--vsock-ctl-port requires a numeric port value in 1...4294967294.")
        }
        vsockCtlPort = port
        vsockCtlPortSet = true
        idx += 2
    } else if arg == "--cpus", idx + 1 < arguments.count {
        guard let n = Int(arguments[idx + 1]), n >= 1, n <= 8 else {
            fail("--cpus requires a count in 1...8, got '\(arguments[idx + 1])'.")
        }
        cpuCount = n
        cpusExplicit = true
        idx += 2
    } else if arg == "--memory-mib", idx + 1 < arguments.count {
        // M129a (#2610): VZ anchors guest RAM at phys 0x7000_0000 growing
        // upward (observed 256/512 MiB boots). The kernel's user-stack
        // ASLR band opens at 4 GiB, so the guest's RAM top
        // (0x7000_0000 + size) must stay below it — 2304 MiB is the hard
        // geometry ceiling; 2048 is the bounded flag value under it.
        guard let n = Int(arguments[idx + 1]), n >= 64, n <= 2048 else {
            fail("--memory-mib requires a size in 64...2048, got '\(arguments[idx + 1])'.")
        }
        memoryMiB = n
        memoryMiBExplicit = true
        idx += 2
    } else if arg == "--timeout", idx + 1 < arguments.count {
        timeout = TimeInterval(arguments[idx + 1]) ?? 30
        timeoutExplicit = true
        idx += 2
    } else if arg == "--expect", idx + 1 < arguments.count {
        expectLine = arguments[idx + 1]
        idx += 2
    } else if arg == "--terminal-marker", idx + 1 < arguments.count {
        terminalMarker = arguments[idx + 1]
        idx += 2
    } else if arg == "--console" {
        consoleMode = true
        idx += 1
    } else if arg == "--console-tcp", idx + 1 < arguments.count {
        // #1066 Stage 0 / M46 RC1 (#1110, ADR 0022 D2): `[host:]port[:secret]`
        // — bind host (default 127.0.0.1) and serve the guest console over
        // TCP. The optional secret gates the client's first line. Implies
        // console mode (duplex serial attachment + guest-output tee).
        let spec = arguments[idx + 1]
        let parts = spec.split(separator: ":", omittingEmptySubsequences: false).map(String.init)
        var parsed = false
        switch parts.count {
        case 1:
            if let p = UInt16(parts[0]) { consoleTCPPort = p; parsed = true }
        case 2:
            if let p = UInt16(parts[0]) { // port:secret
                consoleTCPPort = p; consoleTCPSecret = parts[1]; parsed = true
            } else if let p = UInt16(parts[1]) { // host:port
                consoleTCPBind = parts[0]; consoleTCPPort = p; parsed = true
            }
        case 3:
            if let p = UInt16(parts[1]) { // host:port:secret
                consoleTCPBind = parts[0]; consoleTCPPort = p; consoleTCPSecret = parts[2]; parsed = true
            }
        default:
            break
        }
        // M70g G2 (#1459): never echo the spec back — it may carry the secret.
        guard parsed else { fail("--console-tcp: invalid spec (want [host:]port[:secret])") }
        if consoleTCPBind.isEmpty { consoleTCPBind = "127.0.0.1" }
        if consoleTCPSecret?.isEmpty == true { consoleTCPSecret = nil }
        consoleMode = true
        idx += 2
    } else if arg == "--console-tcp-secret-file", idx + 1 < arguments.count {
        // M70g G2 (#1459): the bridge secret from a file (first line, CR/LF
        // trimmed) so it is visible neither in `ps` nor in any log line.
        let path = arguments[idx + 1]
        guard let raw = FileManager.default.contents(atPath: path) else {
            fail("--console-tcp-secret-file: cannot read '\(path)'")
        }
        let firstLine = raw.split(separator: 0x0a, maxSplits: 1, omittingEmptySubsequences: false).first ?? Data()
        let trimmed = firstLine.last == 0x0d ? firstLine.dropLast() : firstLine
        guard !trimmed.isEmpty else { fail("--console-tcp-secret-file: '\(path)' has an empty first line") }
        consoleTCPSecretFromFile = String(decoding: trimmed, as: UTF8.self)
        idx += 2
    } else if arg == "--debug-input" {
        debugInput = true
        idx += 1
    } else if arg == "--dump-marker", idx + 1 < arguments.count {
        markerDumpPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--nvram-console", idx + 1 < arguments.count {
        nvramConsolePath = arguments[idx + 1]
        idx += 2
    } else if arg == "--script", idx + 1 < arguments.count {
        scriptPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--script-after", idx + 1 < arguments.count {
        scriptAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--script-expect", idx + 1 < arguments.count {
        scriptExpect = arguments[idx + 1]
        idx += 2
    } else if arg == "--script-expect-tail", idx + 1 < arguments.count {
        // Claim 4912: seconds to hold the VM after --script-expect first
        // matches, so post-marker kernel output (the async reap/report
        // lines, drained on the guest's 1 Hz timer tick) reaches the serial
        // log before teardown. 0 = legacy stop-on-first-match.
        scriptExpectTail = Double(arguments[idx + 1]) ?? 1.5
        idx += 2
    } else if arg == "--script2", idx + 1 < arguments.count {
        script2Path = arguments[idx + 1]
        idx += 2
    } else if arg == "--script2-after", idx + 1 < arguments.count {
        script2After = arguments[idx + 1]
        idx += 2
    } else if arg == "--script3", idx + 1 < arguments.count {
        script3Path = arguments[idx + 1]
        idx += 2
    } else if arg == "--script3-after", idx + 1 < arguments.count {
        script3After = arguments[idx + 1]
        idx += 2
    } else if arg == "--custom-virtio" {
        customVirtioEnabled = true
        idx += 1
    } else if arg == "--cvc-echo" {
        // Claim 3141: the host-push echo spike — implies the device attach.
        customVirtioEnabled = true
        cvcEchoEnabled = true
        idx += 1
    } else if arg == "--via-virtio" {
        // Claim 9588: keyboard injection over the custom-virtio INPUT queue
        // (queue 3). Virtqueues are contiguous, so the four-queue shape
        // implies --custom-virtio AND the claim-3141 push echo.
        viaVirtioEnabled = true
        customVirtioEnabled = true
        cvcEchoEnabled = true
        idx += 1
    } else if arg == "--pointer-virtio", idx + 1 < arguments.count {
        // Claim 9367: pointer injection over the custom-virtio INPUT queue
        // (kind-2 absolute-pointer messages) — implies --via-virtio.
        pointerVirtioScript = arguments[idx + 1]
        viaVirtioEnabled = true
        customVirtioEnabled = true
        cvcEchoEnabled = true
        idx += 2
    } else if arg == "--pointer-virtio-after", idx + 1 < arguments.count {
        pointerVirtioAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--cvc-snap" {
        // Claim 0680: the five-queue shape — adds the framebuffer-snapshot
        // queue. Implies --via-virtio (contiguous queues).
        cvcSnapEnabled = true
        viaVirtioEnabled = true
        customVirtioEnabled = true
        cvcEchoEnabled = true
        idx += 1
    } else if arg == "--cvc-file", idx + 1 < arguments.count {
        // M34 HF1+HF2 (issues #735/#736): the SIX-queue shape — attaches
        // the host file channel (queue 5) serving the given macOS folder.
        // The deepest flag implies the full shape below it.
        cvcFileShareDir = arguments[idx + 1]
        cvcSnapEnabled = true
        viaVirtioEnabled = true
        customVirtioEnabled = true
        cvcEchoEnabled = true
        idx += 2
    } else if arg == "--host-clipboard" {
        // M108b (#2325, ADR 0051): the bidirectional clipboard lane —
        // signed lane files under the share's HOSTINT/. It does NOT touch
        // the queue shape: the share (queue 5) is already the transport.
        hostClipboardFlag = true
        idx += 1
    } else if arg == "--host-pasteboard", idx + 1 < arguments.count {
        hostPasteboardName = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-clip-in", idx + 1 < arguments.count {
        hostClipInText = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-clip-in-after", idx + 1 < arguments.count {
        hostClipInAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-clip-in-file", idx + 1 < arguments.count {
        hostClipInFile = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-clip-in-mime", idx + 1 < arguments.count {
        hostClipInMime = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-drop" {
        // M108c (#2326, ADR 0051 D4): the host→guest file-drop lane —
        // INBOX/ writes + signed DROP notices over the same share. Does
        // NOT touch the queue shape either.
        hostDropFlag = true
        idx += 1
    } else if arg == "--host-drop-file", idx + 1 < arguments.count {
        hostDropFiles.append(arguments[idx + 1])
        idx += 2
    } else if arg == "--host-drop-after", idx + 1 < arguments.count {
        hostDropAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-capture-keys" {
        hostCaptureKeys = true
        idx += 1
    } else if arg == "--host-cmd-after", idx + 1 < arguments.count {
        // M114a: <marker>:<verb> — the verb is the suffix after the LAST
        // colon (serial markers may carry their own). An unknown verb is
        // a parse-time error, not a trigger that silently never fires.
        guard let trigger = HostCommandTrigger(arguments[idx + 1]) else {
            fail("--host-cmd-after expects <marker>:<verb>; verbs are: \(HostCommand.allCases.map(\.rawValue).joined(separator: ", "))")
        }
        hostCmdTriggers.append(trigger)
        idx += 2
    } else if arg == "--host-serial-send", idx + 1 < arguments.count {
        // M114d (#2457): arms the inspector's input line — a
        // `--host-cmd-after …:send-serial` trigger writes it into the
        // console pipe, the same path --script forwards through.
        hostSerialSend = arguments[idx + 1]
        inspector.inputLine = hostSerialSend!
        idx += 2
    } else if arg == "--host-notify" {
        // M108d (#2328, ADR 0051 D5): the notify-out lane — seat toasts
        // become signed NOTIFY_OUT records the runner maps to banners
        // (bundled) or no_bundle drops (bare). Modifier on --cvc-file;
        // never touches the queue shape.
        hostNotifyFlag = true
        idx += 1
    } else if arg == "--host-outbox" {
        // M127d (#2602, ADR 0051 V2-D5): the guest→host export lane —
        // REQ-marked payloads under OUTBOX/ become signed records the
        // runner verifies and moves out. Modifier on --cvc-file.
        hostOutboxFlag = true
        idx += 1
    } else if arg == "--host-outbox-dir", idx + 1 < arguments.count {
        hostOutboxDir = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-print" {
        // M202c: arm the print lane — signed PRINT records naming
        // PREQ-staged PDFs under <share>/PRINT/.
        hostPrintFlag = true
        idx += 1
    } else if arg == "--host-print-dir", idx + 1 < arguments.count {
        hostPrintDir = arguments[idx + 1]
        idx += 2
    } else if arg == "--host-speech" {
        // M186d (#3181, ADR 0051 V2-D8): the speech lane — SAY-marked
        // utterances become signed SPEAK records the runner feeds
        // NSSpeechSynthesizer. Modifier on --cvc-file.
        hostSpeechFlag = true
        idx += 1
    } else if arg == "--cvc-console-file", idx + 1 < arguments.count {
        // Claim 0680: structured console — capture every queue-1 guest log
        // line to this file and arm the guest's console tee (kind-3) when
        // it signals readiness.
        cvcConsoleFilePath = arguments[idx + 1]
        customVirtioEnabled = true
        idx += 2
    } else if arg == "--snapshot-after", idx + 1 < arguments.count {
        // Claim 0680: fire one kind-4 snapshot request when this serial
        // marker appears (repeatable; outputs are numbered per instance).
        snapshotAfterMarkers.append(arguments[idx + 1])
        cvcSnapEnabled = true
        viaVirtioEnabled = true
        customVirtioEnabled = true
        cvcEchoEnabled = true
        idx += 2
    } else if arg == "--snapshot-out", idx + 1 < arguments.count {
        snapshotOutBase = arguments[idx + 1]
        idx += 2
    } else if arg == "--snapshot-drain-timeout", idx + 1 < arguments.count {
        // Issue #1788: seconds to hold the VM at teardown while an armed
        // kind-4 snapshot stream is still in flight. Clamp a negative value
        // to 0 here so `snapshotDrainTimeout > 0` in the drain is the
        // documented "do not wait" switch rather than a silent fallthrough:
        // a gate that fat-fingers `-5` gets the legacy stop, not a drain
        // that exits instantly on the first poll.
        snapshotDrainTimeout = Double(arguments[idx + 1]) ?? 20
        if snapshotDrainTimeout < 0 { snapshotDrainTimeout = 0 }
        idx += 2
    } else if arg == "--gpu3d-metal" {
        // M102d3 (#2168): opt-in private 3D render device (custom-virtio
        // 0x43 / PCI 1af4:1083, VIR1) beside native 2D — appended, never
        // replacing. Needs the SPIKE build; checked below with a named
        // failure on an ordinary binary.
        gpu3dOptions.metal = true
        idx += 1
    } else if arg == "--gpu3d-test-mode", idx + 1 < arguments.count {
        // M102d3 (#2168): completion-fault selection for c2's
        // deterministic knobs. SPIKE-only — the pre-attachment check
        // rejects it by name on an ordinary binary.
        guard let mode = GPU3DRunnerConfiguration.TestMode(rawValue: arguments[idx + 1]) else {
            fail("--gpu3d-test-mode: unknown mode '\(arguments[idx + 1])' (want \(GPU3DRunnerConfiguration.TestMode.allCases.map(\.rawValue).joined(separator: "|")))")
        }
        gpu3dOptions.testMode = mode
        idx += 2
    } else if arg == "--gpu3d-test-mode" {
        fail("--gpu3d-test-mode requires a mode (want \(GPU3DRunnerConfiguration.TestMode.allCases.map(\.rawValue).joined(separator: "|")))")
    } else if arg == "--net", idx + 1 < arguments.count {
        netCapturePath = arguments[idx + 1]
        idx += 2
    } else if arg == "--mac", idx + 1 < arguments.count {
        // M206a (#3813, ADR 0057): replace the pinned guest MAC. Locally
        // administered unicast only — each other shape refuses by name.
        let token = arguments[idx + 1]
        switch netMacParse(token) {
        case .ok(let mac):
            netMacOverride = mac
            netTcpGuestMAC = mac
        case .malformed:
            fail("--mac requires a MAC address like 02:00:00:00:00:0a (six colon-separated hex octets), got '\(token)'.")
        case .multicast:
            fail("--mac requires a unicast address (the multicast bit is set in '\(token)').")
        case .notLocal:
            fail("--mac requires a locally administered address (the u/l bit is clear in '\(token)').")
        }
        idx += 2
    } else if arg == "--net-hub", idx + 1 < arguments.count {
        // M206a (#3813): relay guest TX to the host-side L2 hub at this
        // UNIX datagram socket; hub frames come back into the socketpair.
        netHubPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-inject", idx + 1 < arguments.count {
        netInjectPath = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-inject-after", idx + 1 < arguments.count {
        netInjectAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-arp-respond", idx + 1 < arguments.count {
        // Parse the dotted-quad host IP now (fail early, like --timeout).
        let parts = arguments[idx + 1].split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-arp-respond requires a dotted-quad IPv4 address, got '\(arguments[idx + 1])'.")
        }
        netArpRespondHostIP = parts
        idx += 2
    } else if arg == "--net-icmp-respond", idx + 1 < arguments.count {
        // Parse the dotted-quad host IP now (fail early, like --timeout).
        let parts = arguments[idx + 1].split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-icmp-respond requires a dotted-quad IPv4 address, got '\(arguments[idx + 1])'.")
        }
        netIcmpRespondHostIP = parts
        idx += 2
    } else if arg == "--net-ra-respond", idx + 1 < arguments.count {
        // M117b: <v6-prefix>/64 — e.g. 2001:db8:64::/64 (the advertised
        // PIO; the IID bits belong to the guest, the responder zeroes
        // them so the advertised prefix is honest).
        let halves = arguments[idx + 1].split(separator: "/")
        guard halves.count == 2, Int(halves[1]) == 64,
              var pfx = parseIPv6(String(halves[0])) else {
            fail("--net-ra-respond requires a v6 /64 prefix like 2001:db8:64::/64, got '\(arguments[idx + 1])'.")
        }
        for i in 8..<16 { pfx[i] = 0 }
        netRaRespondPrefix = pfx
        idx += 2
    } else if arg == "--net-ndp-respond", idx + 1 < arguments.count {
        // M117b: repeatable — each instance lists one answerable target.
        guard let addr = parseIPv6(arguments[idx + 1]) else {
            fail("--net-ndp-respond requires a v6 address, got '\(arguments[idx + 1])'.")
        }
        netNdpRespondAddrs.append(addr)
        idx += 2
    } else if arg == "--net-icmp6-respond", idx + 1 < arguments.count {
        guard let addr = parseIPv6(arguments[idx + 1]) else {
            fail("--net-icmp6-respond requires a v6 address, got '\(arguments[idx + 1])'.")
        }
        netIcmp6RespondAddr = addr
        idx += 2
    } else if arg == "--net-udp6-respond", idx + 1 < arguments.count {
        // M117c (#2475): '[v6-addr]:port' — the bracket keeps the
        // address's own colons out of the port split.
        let token = arguments[idx + 1]
        guard token.hasPrefix("["), let close = token.firstIndex(of: "]") else {
            fail("--net-udp6-respond requires '[<v6-addr>]:<host-port>', got '\(token)'.")
        }
        let addrText = String(token[token.index(after: token.startIndex)..<close])
        guard let addr = parseIPv6(addrText),
              token[token.index(after: close)..<token.endIndex].hasPrefix(":"),
              let port = UInt16(token[token.index(after: close)..<token.endIndex].dropFirst()) else {
            fail("--net-udp6-respond requires '[<v6-addr>]:<host-port>', got '\(token)'.")
        }
        netUdp6RespondAddr = addr
        netUdp6RespondPort = port
        idx += 2
    } else if arg == "--net-tcp6-respond", idx + 1 < arguments.count {
        // M117c (#2475): the same '[v6-addr]:port' bracketed shape.
        let token = arguments[idx + 1]
        guard token.hasPrefix("["), let close = token.firstIndex(of: "]") else {
            fail("--net-tcp6-respond requires '[<v6-addr>]:<host-port>', got '\(token)'.")
        }
        let addrText = String(token[token.index(after: token.startIndex)..<close])
        guard let addr = parseIPv6(addrText),
              token[token.index(after: close)..<token.endIndex].hasPrefix(":"),
              let port = UInt16(token[token.index(after: close)..<token.endIndex].dropFirst()) else {
            fail("--net-tcp6-respond requires '[<v6-addr>]:<host-port>', got '\(token)'.")
        }
        netTcp6RespondAddr = addr
        netTcp6RespondPort = port
        idx += 2
    } else if arg == "--net-udp-respond", idx + 1 < arguments.count {
        // Parse the host ip:port now (fail early, like --timeout).
        let halves = arguments[idx + 1].split(separator: ":")
        guard halves.count == 2, let port = UInt16(halves[1]) else {
            fail("--net-udp-respond requires '<host-ip>:<host-port>', got '\(arguments[idx + 1])'.")
        }
        let parts = halves[0].split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-udp-respond requires a dotted-quad IPv4 address, got '\(arguments[idx + 1])'.")
        }
        netUdpRespondHostIP = parts
        netUdpRespondHostPort = port
        idx += 2
    } else if arg == "--net-dns-respond", idx + 1 < arguments.count {
        let token = arguments[idx + 1]
        let halves = token.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let ipPart = String(halves[0])
        let parts = ipPart.split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-dns-respond requires a dotted-quad IPv4 address, got '\(token)'.")
        }
        netDnsRespondHostIP = parts
        if halves.count == 2, let port = UInt16(halves[1]) {
            netDnsRespondHostPort = port
        } else if halves.count == 2 {
            fail("--net-dns-respond port must be 1..65535, got '\(halves[1])'.")
        }
        idx += 2
    } else if arg == "--net-dns-answer", idx + 1 < arguments.count {
        // M99i (issue #2187): pin an A record into every A reply.
        let parts = arguments[idx + 1].split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-dns-answer requires a dotted-quad IPv4 address, got '\(arguments[idx + 1])'.")
        }
        netDnsAnswers.append(parts)
        idx += 2
    } else if arg == "--net-dns-answer6", idx + 1 < arguments.count {
        // M99i: pin an AAAA record into every AAAA reply — 32 hex
        // digits (colons tolerated, stripped).
        let hex = arguments[idx + 1].replacingOccurrences(of: ":", with: "")
        guard hex.count == 32, let rdata = parseHexBytes(hex), rdata.count == 16 else {
            fail("--net-dns-answer6 requires 32 hex digits (colons optional), got '\(arguments[idx + 1])'.")
        }
        netDnsAnswers6.append(rdata)
        idx += 2
    } else if arg == "--net-dns-ttl", idx + 1 < arguments.count {
        guard let ttl = UInt32(arguments[idx + 1]) else {
            fail("--net-dns-ttl requires a non-negative integer, got '\(arguments[idx + 1])'.")
        }
        netDnsTTL = ttl
        idx += 2
    } else if arg == "--net-dns-spoof" {
        netDnsSpoof = true
        idx += 1
    } else if arg == "--net-dns-wrong-question" {
        netDnsWrongQuestion = true
        idx += 1
    } else if arg == "--net-dns-truncate" {
        netDnsTruncate = true
        idx += 1
    } else if arg == "--net-sntp-respond", idx + 1 < arguments.count {
        let token = arguments[idx + 1]
        let halves = token.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let octets = String(halves[0]).split(separator: ".", omittingEmptySubsequences: false).compactMap { UInt8($0) }
        guard octets.count == 4, halves[0].split(separator: ".", omittingEmptySubsequences: false).count == 4 else {
            fail("--net-sntp-respond requires a dotted-quad IPv4 address, got '\(token)'.")
        }
        netSntpRespondHostIP = octets
        if halves.count == 2 {
            guard let port = UInt16(halves[1]), port >= 1 else {
                fail("--net-sntp-respond port must be 1..65535, got '\(halves[1])'.")
            }
            netSntpRespondHostPort = port
        }
        idx += 2
    } else if arg == "--net-sntp-skew", idx + 1 < arguments.count {
        guard let skew = Int64(arguments[idx + 1]) else {
            fail("--net-sntp-skew requires a signed integer number of seconds, got '\(arguments[idx + 1])'.")
        }
        netSntpSkew = skew
        idx += 2
    } else if arg == "--net-dhcp-respond", idx + 1 < arguments.count {
        // Card N9 (claim 9489): the optional ":<lease-seconds>" suffix
        // (default 3600 — backward compatible) makes the OFFER/ACK's
        // lease option 51 configurable, so a live gate can test the
        // lease lifecycle (renewal/rebind/expiry) in seconds. Parse now
        // (fail early, like --timeout).
        let token = arguments[idx + 1]
        let halves = token.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let ipPart = String(halves[0])
        let parts = ipPart.split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-dhcp-respond requires a dotted-quad IPv4 lease address, got '\(token)'.")
        }
        netDhcpRespondLeaseIP = parts
        if halves.count == 2, let lease = UInt32(halves[1]), lease >= 1, lease <= 86400 {
            netDhcpRespondLeaseSecs = lease
        } else if halves.count == 2 {
            fail("--net-dhcp-respond lease must be 1..86400 seconds, got '\(halves[1])'.")
        }
        idx += 2
    } else if arg == "--net-dhcp-respond-norenew" {
        // Audit follow-up 3 (issue #119): refuse the guest's unicast
        // RENEWING REQUESTs (the host keeps answering DISCOVERs + the
        // initial/rebinding broadcast REQUESTs).
        netDhcpRespondNoRenew = true
        idx += 1
    } else if arg == "--net-dhcp-respond-norebind" {
        // Audit follow-up 3 (issue #119): refuse the guest's broadcast
        // REBINDING REQUESTs (ciaddr != 0). The initial REQUEST
        // (ciaddr == 0) is still answered, so binds/recovery work.
        netDhcpRespondNoRebind = true
        idx += 1
    } else if arg == "--net-tcp-respond", idx + 1 < arguments.count {
        // Card N10 (claim 7026): the same shape as --net-udp-respond
        // (host-ip:host-port). Card N11 (claim 5357): an optional
        // `:handshake` suffix selects the handshake-only responder (SYN
        // -> SYN-ACK, then silent on data/FIN — the gate's deterministic
        // black hole). M51 SSH1 (#1168): a `:packet` suffix selects the
        // paced packet responder (see --net-tcp-respond-payload). Parse now
        // (fail early, like --timeout).
        let token = arguments[idx + 1]
        let halves = token.split(separator: ":", maxSplits: 2, omittingEmptySubsequences: false)
        guard halves.count >= 2, let port = UInt16(halves[1]) else {
            fail("--net-tcp-respond requires <host-ip>:<host-port>[:handshake|packet|ssh|relay|options], got '\(token)'.")
        }
        if halves.count == 3 {
            if halves[2] == "handshake" {
                netTcpRespondHandshakeOnly = true
            } else if halves[2] == "packet" {
                netTcpRespondPacketMode = true
            } else if halves[2] == "ssh" {
                // M51 SSH5 (#1172): the minimal SSH-2 server.
                netTcpRespondSSHMode = true
            } else if halves[2] == "relay" {
                // M51 interop (#1209): byte-forward to a real upstream
                // (`--net-tcp-respond-relay`).
                netTcpRespondRelayMode = true
            } else if halves[2] == "options" {
                // M99h (#2186): the option-carrying peer (a real peer's
                // wire shape — the guest must parse-and-skip).
                netTcpRespondOptionsMode = true
            } else {
                fail("--net-tcp-respond mode must be 'handshake', 'packet', 'ssh', 'relay' or 'options', got '\(halves[2])'.")
            }
        }
        let parts = halves[0].split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-tcp-respond requires a dotted-quad IPv4 address, got '\(token)'.")
        }
        netTcpRespondHostIP = parts
        netTcpRespondHostPort = port
        idx += 2
    } else if arg == "--net-tcp-respond-payload", idx + 1 < arguments.count {
        // M51 SSH1 (#1168): the byte payload the `:packet` mode paces out.
        do {
            netTcpRespondPacketPayload = [UInt8](try Data(contentsOf: URL(fileURLWithPath: arguments[idx + 1])))
        } catch {
            fail("--net-tcp-respond-payload could not read file '\(arguments[idx + 1])': \(error).")
        }
        idx += 2
    } else if arg == "--net-tcp-respond-chunk", idx + 1 < arguments.count {
        guard let n = Int(arguments[idx + 1]), n >= 1, n <= 192 else {
            fail("--net-tcp-respond-chunk requires 1..192 bytes, got '\(arguments[idx + 1])'.")
        }
        netTcpRespondPacketChunk = n
        idx += 2
    } else if arg == "--net-tcp-respond-ssh-hostkey", idx + 1 < arguments.count {
        // M51 SSH5 (#1172): the pinned ssh-ed25519 host key seed (64 hex
        // chars). Default: RFC 8032 §7.1 TEST 1.
        guard let seed = SSHFixtures.hex(arguments[idx + 1]), seed.count == 32 else {
            fail("--net-tcp-respond-ssh-hostkey requires 64 hex chars (a 32-byte Ed25519 seed), got '\(arguments[idx + 1])'.")
        }
        netTcpRespondSSHHostKey = seed
        idx += 2
    } else if arg == "--net-tcp-respond-ssh-userkey", idx + 1 < arguments.count {
        // M51 SSH5 (#1172): the pinned client ssh-ed25519 PUBLIC key (64 hex
        // chars) the server accepts. Default: RFC 8032 §7.1 TEST 2.
        guard let key = SSHFixtures.hex(arguments[idx + 1]), key.count == 32 else {
            fail("--net-tcp-respond-ssh-userkey requires 64 hex chars (a 32-byte Ed25519 public key), got '\(arguments[idx + 1])'.")
        }
        netTcpRespondSSHUserKey = key
        idx += 2
    } else if arg == "--net-tcp-respond-ssh-tamper-mac" {
        // NEGATIVE MODE: flip one bit of the first encrypted server tag so
        // the guest's AEAD open must fail closed.
        netTcpRespondSSHTamperMAC = true
        idx += 1
    } else if arg == "--net-tcp-respond-ssh-marker", idx + 1 < arguments.count {
        netTcpRespondSSHMarker = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-tcp-respond-ssh-exit", idx + 1 < arguments.count {
        guard let n = UInt32(arguments[idx + 1]) else {
            fail("--net-tcp-respond-ssh-exit requires a uint32, got '\(arguments[idx + 1])'.")
        }
        netTcpRespondSSHExitStatus = n
        idx += 2
    } else if arg == "--net-tcp-respond-relay", idx + 1 < arguments.count {
        // M51 interop (#1209): the real upstream the `:relay` mode
        // byte-forwards to, `<host-ip>:<host-port>` (dotted quad, like the
        // other responders — the documented manual real-sshd check).
        let token = arguments[idx + 1]
        let halves = token.split(separator: ":", maxSplits: 2, omittingEmptySubsequences: false)
        guard halves.count >= 2, let port = UInt16(halves[1]) else {
            fail("--net-tcp-respond-relay requires <host-ip>:<host-port>, got '\(token)'.")
        }
        let parts = halves[0].split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-tcp-respond-relay requires a dotted-quad IPv4 address, got '\(token)'.")
        }
        netTcpRespondRelayHost = String(halves[0])
        netTcpRespondRelayPort = port
        idx += 2
    } else if arg == "--net-tcp-connect", idx + 1 < arguments.count {
        // SH7 (#1083, ADR 0020 Amendment B): the host-initiated inbound TCP
        // client, the reverse of --net-tcp-respond. The optional third
        // component is a payload file whose bytes are sent once the
        // handshake completes.
        let token = arguments[idx + 1]
        let halves = token.split(separator: ":", maxSplits: 2, omittingEmptySubsequences: false)
        guard halves.count >= 2, let port = UInt16(halves[1]) else {
            fail("--net-tcp-connect requires <guest-ip>:<port>[:<payload-file>], got '\(token)'.")
        }
        let parts = halves[0].split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else {
            fail("--net-tcp-connect requires a dotted-quad guest IPv4 address, got '\(token)'.")
        }
        var payload: [UInt8] = []
        var sshMode = false
        if halves.count == 3, !halves[2].isEmpty {
            if halves[2] == "ssh" {
                // M70g G1 (#1491): host SSH-2 client against GOSSHD.ELF.
                sshMode = true
                netTcpConnectSSHMode = true
            } else {
                let payloadPath = String(halves[2])
                do {
                    payload = [UInt8](try Data(contentsOf: URL(fileURLWithPath: payloadPath)))
                } catch {
                    fail("--net-tcp-connect could not read payload file '\(payloadPath)': \(error).")
                }
            }
        }
        // M99f (#2184): one NetTcpClient per flag — several host-initiated
        // connections run concurrently, each on its own source port.
        netTcpClients.append(NetTcpClient(
            guestIP: parts,
            guestPort: port,
            cliPort: netTcpCliPort &+ UInt16(netTcpClients.count),
            payload: payload,
            sshMode: sshMode
        ))
        if netTcpClients.count == 1 {
            netTcpConnectGuestIP = parts
            netTcpConnectPort = port
            netTcpConnectPayload = payload
        }
        idx += 2
    } else if arg == "--net-tcp-connect-ssh-exec", idx + 1 < arguments.count {
        netTcpConnectSSHExec = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-tcp-connect-ssh-pty", idx + 1 < arguments.count {
        // M168f (#2962): a scripted interactive session — pty-req
        // (TERM=xterm-256color) + shell replace the exec request.
        let token = arguments[idx + 1]
        let halves = token.split(separator: "x", omittingEmptySubsequences: false)
        guard halves.count == 2,
              let cols = UInt32(halves[0]), let rows = UInt32(halves[1]),
              cols > 0, rows > 0, cols <= 65535, rows <= 65535 else {
            fail("--net-tcp-connect-ssh-pty requires <cols>x<rows>, got '\(token)'.")
        }
        netTcpConnectSSHPty = (cols, rows)
        idx += 2
    } else if arg == "--net-tcp-connect-ssh-keys", idx + 1 < arguments.count {
        // M168f (#2962): the line script — `after <text>` /
        // `send <escaped bytes>` / `resize <cols>x<rows>` — walked against
        // the decrypted channel stream once the shell is live.
        let path = arguments[idx + 1]
        do {
            netTcpConnectSSHKeys = try SSHKeyScript(
                text: String(contentsOfFile: path, encoding: .utf8)
            )
        } catch let err as SSHKeyScript.ParseError {
            fail("--net-tcp-connect-ssh-keys '\(path)': \(err).")
        } catch {
            fail("--net-tcp-connect-ssh-keys could not read '\(path)': \(error).")
        }
        idx += 2
    } else if arg == "--net-tcp-connect-ssh-session-file", idx + 1 < arguments.count {
        // M168f (#2962): CHANNEL_DATA lands here byte-for-byte — the
        // gate's ssh-session-<tag>.bin evidence.
        netTcpConnectSSHSessionFile = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-tcp-connect-stream", idx + 1 < arguments.count {
        netTcpConnectStreamExec = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-tcp-connect-stream-arg", idx + 1 < arguments.count {
        netTcpConnectStreamArgs.append(arguments[idx + 1])
        idx += 2
    } else if arg == "--net-native-preview", idx + 1 < arguments.count {
        let mode = arguments[idx + 1]
        guard mode == "reset" || mode == "timeout" else { fail("--net-native-preview requires reset|timeout") }
        nativeSocketProbeMode = mode
        nativeSocketProbe = NativeSocketProbe(mode: mode)
        idx += 2
    } else if arg == "--net-tcp-connect-resyn", idx + 1 < arguments.count {
        // #3391: re-SYN each eligible client on every new after-marker
        // sighting — sequential refused sessions for the audit-lane spam
        // leg, paced by the daemon's own relisten marker.
        guard let n = Int(arguments[idx + 1]), n >= 0 else {
            fail("--net-tcp-connect-resyn requires a non-negative count, got '\(arguments[idx + 1])'.")
        }
        netTcpConnectResyn = n
        idx += 2
    } else if arg == "--net-tcp-connect-after-skip", idx + 1 < arguments.count {
        // #3391: this client's SYN ignores the first <n> after-marker
        // sightings. Applies to the most recently declared client.
        guard let n = Int(arguments[idx + 1]), n >= 0, let client = netTcpClients.last else {
            fail("--net-tcp-connect-after-skip requires a non-negative count after a --net-tcp-connect, got '\(arguments[idx + 1])'.")
        }
        client.afterSkip = n
        idx += 2
    } else if arg == "--net-tcp-connect-intruder" {
        netTcpConnectIntruder = true
        idx += 1
    } else if arg == "--net-tcp-connect-ssh-user", idx + 1 < arguments.count {
        netTcpConnectSSHUser = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-tcp-connect-after", idx + 1 < arguments.count {
        netTcpConnectAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-tcp-connect-close-after", idx + 1 < arguments.count {
        netTcpConnectCloseAfter = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-tcp-connect-secret", idx + 1 < arguments.count {
        // M50 TS4 (#1138, ADR 0024 D6/D7): answer the guest's
        // `VIRELAIOS-AUTH/1` challenge with HMAC-SHA256(secret, message).
        netTcpConnectSecret = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-tcp-connect-mac", idx + 1 < arguments.count {
        // M50 TS4 replay path: send this pre-recorded MAC hex line verbatim
        // (a captured handshake replayed against the fresh challenge).
        netTcpConnectMac = arguments[idx + 1]
        idx += 2
    } else if arg == "--script2-delay", idx + 1 < arguments.count {
        // Card N9 (claim 9489): the claim-6684 settle before forwarding
        // script2 becomes configurable (flag-gated, default 0.5 — every
        // existing gate is unchanged), so a lease-lifecycle gate can wait
        // past T1/T2/expiry deterministically.
        guard let d = Double(arguments[idx + 1]), d >= 0 else {
            fail("--script2-delay requires a non-negative number of seconds, got '\(arguments[idx + 1])'.")
        }
        script2Delay = d
        idx += 2
    } else if arg == "--script3-delay", idx + 1 < arguments.count {
        guard let d = Double(arguments[idx + 1]), d >= 0 else {
            fail("--script3-delay requires a non-negative number of seconds, got '\(arguments[idx + 1])'.")
        }
        script3Delay = d
        idx += 2
    } else if arg == "--net-nat" {
        netNatEnabled = true
        idx += 1
    } else if arg == "--net-bridged", idx + 1 < arguments.count {
        netBridgedInterface = arguments[idx + 1]
        idx += 2
    } else if arg == "--net-vmnet" {
        netVmnetEnabled = true
        idx += 1
    } else if arg == "--net-vmnet-subnet", idx + 1 < arguments.count {
        // <a.b.c.d>/<len> — the subnet base for the vmnet logical
        // network (the daemon assigns a rotating /24 without it). Must be
        // the network address: the daemon interprets the value literally
        // and a non-base address is a silent different network.
        let halves = arguments[idx + 1].split(separator: "/")
        let parts = halves.count == 2 ? halves[0].split(separator: ".").compactMap { UInt8($0) } : []
        guard halves.count == 2, let len = Int(halves[1]), (1...31).contains(len), parts.count == 4 else {
            fail("--net-vmnet-subnet requires '<a.b.c.d>/<len>' like 192.168.77.0/24, got '\(arguments[idx + 1])'.")
        }
        let mv: UInt32 = ~UInt32(0) << (32 - len)
        let av = UInt32(parts[0]) << 24 | UInt32(parts[1]) << 16 | UInt32(parts[2]) << 8 | UInt32(parts[3])
        guard av & mv == av else {
            fail("--net-vmnet-subnet: '\(arguments[idx + 1])' is not the network base (host bits set).")
        }
        netVmnetSubnet = (parts, len)
        idx += 2
    } else {
        serialLogPath = arg
        idx += 1
    }
}

// M128c (#2607): the packaged session app is the `.virelaivm` document
// handler (Session-Info.plist's CFBundleDocumentTypes). A Finder
// double-click launches it BARE — argv is just the executable — and
// delivers the clicked bundle as the open-documents AppleEvent after
// finishLaunching. Harvest it here, before the boot shape is built, and
// route it through the normal `--profile` resolution below so the bundle
// is minted/refused by name exactly as a flag boot is. The posture is the
// session front door's: --display --input --host-session, no deadline,
// and the serial log inside the clicked bundle's run/ (cwd is not the
// repo under a Finder launch, so the artifacts/ default cannot apply).
// A bare UNBUNDLED run keeps its artifacts/disk.img default — the gate
// fleet's plain VMRunner invocations never enter this block.
if arguments.count == 1, Bundle.main.bundleURL.pathExtension == "app" {
    let opener = HostOpenDocuments()
    let app = NSApplication.shared
    app.setActivationPolicy(.regular)
    app.delegate = opener
    app.finishLaunching()
    HostApplication.launchFinished = true
    // `odoc` arrives through the app's EVENT queue, not a runloop source —
    // RunLoop.run never dispatches it (observed: the harvest timed out,
    // and the queued event landed on the failure window's delegate and
    // was refused). Drive the same loop body NSApp.run() runs.
    let openDeadline = Date().addingTimeInterval(5)
    while opener.openedURL == nil, Date() < openDeadline {
        autoreleasepool {
            if let event = app.nextEvent(
                matching: .any, until: Date().addingTimeInterval(0.05), inMode: .default, dequeue: true) {
                app.sendEvent(event)
            }
        }
    }
    guard let clicked = opener.openedURL else {
        HostApplication.presentLaunchFailure(
            "VirelaiOS was launched without a .virelaivm machine document. Double-click a profile bundle in Finder, or run `just session`.")
    }
    // A Finder launch has no terminal — a corrupt bundle refuses in the
    // session window, not on a stderr nobody can see.
    do {
        _ = try VMProfile.load(from: clicked)
    } catch {
        HostApplication.presentLaunchFailure("\(error)")
    }
    documentProfileURL = clicked
    profilePath = clicked.path
    hostSessionMode = true
    displayMode = true
    inputMode = true
    timeout = 0
    let runDir = clicked.appendingPathComponent("run", isDirectory: true)
    try? FileManager.default.createDirectory(at: runDir, withIntermediateDirectories: true)
    serialLogPath = runDir.appendingPathComponent("serial.log").path
    print("host-open: document \(clicked.path)")
}

// M115c (#2462): `--profile` resolves the bundle BEFORE the member paths
// are consumed below. An absent path is minted; an existing directory must
// carry a readable virelaivm/1 manifest (missing/corrupt refuses by name).
// Every profile member is a default — an explicit --vars/--overlay-base/
// --data-disk/--cvc-file/--cpus flag wins regardless of argument order.
if profileInitOnly, profilePath == nil {
    fail("--profile-init requires --profile <path>.")
}
// M115d (#2463): `--resume` IS a profile boot — it names the bundle and
// flags the saved-state restore below. A resume names an existing
// bundle by name; minting one to restore an empty saved-state would be
// a silent no-suspend, so the load fails closed instead.
if let resumeFromPath {
    if profileInitOnly {
        fail("--resume and --profile-init are mutually exclusive.")
    }
    if vzRestore {
        fail("--resume and --vz-restore are mutually exclusive.")
    }
    if let profilePath, profilePath != resumeFromPath {
        fail("--resume and --profile disagree (resume boots the named bundle; pass one path).")
    }
    profilePath = resumeFromPath
}
if let profilePath {
    do {
        activeProfile = resumeFromPath != nil
            ? try VMProfile.load(from: URL(fileURLWithPath: profilePath))
            : try VMProfile.openOrCreate(at: URL(fileURLWithPath: profilePath))
    } catch {
        fail("\(resumeFromPath != nil ? "--resume" : "--profile"): \(error)")
    }
}
if let _ = resumeFromPath {
    // The file the ctl `suspend` verb writes — its absence is the honest
    // "there is no suspended state" refusal, named up front.
    let url = activeProfile!.savedStateURL.appendingPathComponent(SuspendCtl.stateFileName)
    guard FileManager.default.fileExists(atPath: url.path) else {
        fail("--resume: no saved state at \(url.path) (nothing has suspended into this profile).")
    }
    resumeStateURL = url
    print("SUSPEND-RESUME: armed state=\(url.path)")
}
if let p = activeProfile {
    if varsOverridePath == nil { varsOverridePath = p.efiVarsURL.path }
    if overlayBasePath == nil { overlayBasePath = p.config.baseImage }
    if dataDiskPath == nil, p.config.devices.dataDisk { dataDiskPath = p.vlfsURL.path }
    if cvcFileShareDir == nil, virtioFSShareDir == nil, p.config.devices.share {
        // The bundle's share/ rides the same host file channel --cvc-file
        // attaches — including the implied six-queue device shape. An
        // explicit --virtio-fs wins the slot: the custom-virtio device is
        // the one VZ save/restore refuses, so a save-capable caller arms
        // the share on the standard transport instead (M120a #2558).
        cvcFileShareDir = p.shareURL.path
        cvcSnapEnabled = true
        viaVirtioEnabled = true
        customVirtioEnabled = true
        cvcEchoEnabled = true
    }
    if !cpusExplicit { cpuCount = p.config.cpus }
    if !memoryMiBExplicit { memoryMiB = p.config.memoryMiB }
    displayMode = displayMode || p.config.devices.display
    inputMode = inputMode || p.config.devices.input
    soundMode = soundMode || p.config.devices.sound
    netNatEnabled = netNatEnabled || p.config.devices.netNat
    vsockEnabled = vsockEnabled || p.config.devices.vsock
}
if profileInitOnly {
    // Mint/validate the bundle and exit without booting — session.sh runs
    // this before seeding the share, so a corrupt bundle refuses by name
    // up front instead of mid-boot. The force-unwrap is safe: the guard
    // above already proved a profile loaded.
    let p = activeProfile!
    print("profile: \(p.created ? "created" : "loaded") \(p.url.path)")
    print("profile: identity=\(p.identity) cpus=\(p.config.cpus) memoryMiB=\(p.config.memoryMiB) base=\(p.config.baseImage)")
    exit(0)
}

// M115f (#2465): the opt-in OS self-update legs. `--apply-update` runs
// BEFORE the base image is opened: verify the signed staged set on the
// armed share, then swap it in as the base (old base -> <base>.prev, a
// profile's persistent overlay.asif discarded). `--rollback-update`
// swaps .prev back. Both are pure file operations on host paths — the
// guest can stage but can never choose whether the apply happens.
if applyUpdate, rollbackUpdate {
    fail("--apply-update and --rollback-update are mutually exclusive.")
}
if applyUpdate || rollbackUpdate {
    guard let base = overlayBasePath else {
        fail("\(rollbackUpdate ? "--rollback-update" : "--apply-update"): no_base (no base image — needs --overlay-base or --profile).")
    }
    let baseURL = URL(fileURLWithPath: base)
    // A profile's persistent overlay belongs to the outgoing base — a
    // swapped base starts with a fresh layer (the overlay is re-minted at
    // the attach point below when absent).
    let overlayURL = activeProfile?.overlayURL
    if rollbackUpdate {
        let hadOverlay = overlayURL.map { FileManager.default.fileExists(atPath: $0.path) } ?? false
        do {
            let sha = try OSUpdate.rollback(baseImage: baseURL, overlay: overlayURL)
            print("apply-update: rollback restored sha256=\(sha)")
            if hadOverlay {
                print("apply-update: overlay reset (stale sectors belong to the old base)")
            }
        } catch {
            fail("apply-update: \(error)")
        }
    } else {
        // The staged set lives on the share the guest writes — the armed
        // --cvc-file / --virtio-fs dir or the bundle's share/.
        guard let share = cvcFileShareDir ?? virtioFSShareDir else {
            fail("apply-update: no_share (UPDATE/ is staged on the host share — arm --cvc-file/--virtio-fs or a profile share).")
        }
        if let updateDir = OSUpdate.stagedDir(shareDir: URL(fileURLWithPath: share)) {
            // The trust pin: the flag wins; else the bundle's own
            // config.json updatePubkey — a host-side value the guest
            // cannot reach.
            let keyHex = updateKeyHex ?? activeProfile?.config.updatePubkey
            guard let keyHex, let keyData = dataFromHex(keyHex), keyData.count == 32 else {
                fail("apply-update: no_key (--update-key <64-hex Ed25519 public key> or profile config updatePubkey).")
            }
            // The overlay marker is observable: the discard of a
            // persistent overlay.asif is the proof no stale sector shadows
            // the new base (the attach point re-mints it on THIS boot).
            let hadOverlay = overlayURL.map { FileManager.default.fileExists(atPath: $0.path) } ?? false
            do {
                let manifest = try OSUpdate.apply(updateDir: updateDir, baseImage: baseURL,
                                                  publicKey: keyData, overlay: overlayURL)
                print("apply-update: applied version=\(manifest.version) image=\(OSUpdate.imageName)")
                if hadOverlay {
                    print("apply-update: overlay reset (stale sectors belong to the old base)")
                }
            } catch {
                fail("apply-update: \(error)")
            }
        } else {
            print("apply-update: none staged")
        }
    }
}


// M114a: --host-cmd-after's marker scan runs on the scripted/session
// polls; refuse silently-never-firing modes up front.
if !hostCmdTriggers.isEmpty, !scriptMode, !hostSessionMode {
    fail("--host-cmd-after requires --script or --host-session (the serial-marker scan runs on the scripted and session polls).")
}
// M114b: --host-capture-keys needs a VZVirtualMachineView to capture keys
// into (--host-session implies --display by its own validation).
if hostCaptureKeys, screenshotPath == nil, !displayMode {
    fail("--host-capture-keys requires --screen/--display or --host-session (there is no VZVirtualMachineView to capture keys into).")
}
// M114d: the injector is single-line — a newline would smuggle extra
// commands past the drawer's one-line contract.
if let line = hostSerialSend, line.contains("\n") {
    fail("--host-serial-send takes a single line (no newline).")
}


atexit { restoreTerminal() }

atexit {
    if let p = overlayCleanupPath, ProcessInfo.processInfo.environment["VIRELAI_KEEP_OVERLAY"] == nil {
        try? FileManager.default.removeItem(atPath: p)
    }
}


guard hostArchitecture() == "arm64" else {
    fail("Unsupported host architecture '\(hostArchitecture())' -- Apple silicon is required.")
}
// Project requirement: macOS 27+ (Apple silicon + Virtualization.framework).
// macOS 27 is the floor — the host-side custom-virtio interrupt path
// (VZCustomVirtioDevice) and the project's SDK/toolchain target assume it.
guard osVersion.majorVersion >= 27 else {
    fail("macOS \(osVersion.majorVersion) is too old — this project requires macOS 27 or newer (Apple silicon + Virtualization.framework).")
}
if hostSessionMode {
    guard displayMode && inputMode && !consoleMode && !vzRestore else {
        fail("--host-session requires --display --input and cannot be used with --console or --vz-restore.")
    }
}

if vzRestore, !timeout.isFinite || timeout <= 0 || timeout > 600 {
    fail("--vz-restore requires --timeout in (0, 600] seconds; it never runs unbounded.")
}
if vsockPortSet, !vsockEnabled {
    fail("--vsock-port requires --vsock (the port names the host listener on the attached device).")
}
if vsockCtlEnabled, !vsockEnabled {
    fail("--vsock-ctl requires --vsock (the control channel rides the same socket device).")
}
if vsockCtlPortSet, !vsockCtlEnabled {
    fail("--vsock-ctl-port requires --vsock-ctl.")
}
if vzRestoreWitness != nil, !vzRestore {
    fail("--vz-restore-witness requires --vz-restore.")
}
if let witness = vzRestoreWitness, witness.isEmpty || witness.contains("\n") {
    fail("--vz-restore-witness requires one non-empty serial line.")
}
if vzRestoreVerifyCommand != nil || vzRestoreVerifyOutput != nil {
    guard vzRestore, vzRestoreWitness != nil, vzRestoreSaveDir == nil,
          let command = vzRestoreVerifyCommand, !command.isEmpty, !command.contains("\n"),
          let output = vzRestoreVerifyOutput, !output.isEmpty, !output.contains("\n") else {
        fail("--vz-restore-verify-command and --vz-restore-verify-output require a witness and a run that resumes the VM.")
    }
}
// M70g G3 (#1459): the cross-process pair is two processes with one role each.
if vzRestoreSaveDir != nil || vzRestoreLoadDir != nil {
    guard vzRestore else { fail("--vz-restore-save/--vz-restore-load require --vz-restore.") }
    guard vzRestoreSaveDir == nil || vzRestoreLoadDir == nil else {
        fail("--vz-restore-save and --vz-restore-load are mutually exclusive (one process saves, another loads).")
    }
}
// ADR 0059 (M131b #2639): the scripted warm-leg shape. --script under
// --vz-restore is only meaningful on the load side — a save or
// same-process run has no checkpoint to resume into a leg.
if vzRestore, scriptPath != nil, vzRestoreLoadDir == nil {
    fail("--script under --vz-restore requires --vz-restore-load — a scripted leg restores a checkpoint bundle; the save/same-process probes run no leg workload (ADR 0059 D2/D4).")
}
if vzWarmLeg, vzRestoreWitness != nil {
    fail("--vz-restore-witness + --script: a scripted warm leg's handshake replaces the probe's marker query (ADR 0059 D5) — pick one load path.")
}
if vzRestoreSettle != nil {
    guard vzRestore, vzRestoreSaveDir != nil else {
        fail("--vz-restore-settle requires --vz-restore-save — it names the base boot's quiescent marker (ADR 0059 D4).")
    }
    guard !vzRestoreSettle!.isEmpty, !vzRestoreSettle!.contains("\n") else {
        fail("--vz-restore-settle requires one non-empty serial marker.")
    }
}
// M113b (#2418): the verified-boot flag family is all-or-nothing —
// enrollment flags without the arming flag are a loud error, never a
// silently ignored flag (the hardware-contract convention).
if !secureBootEnabled, secureBootPKPath != nil || !secureBootDbEfiPaths.isEmpty || !secureBootDbHashes.isEmpty {
    fail("--secure-boot-pk/--secure-boot-db-efi/--secure-boot-db-hash require --secure-boot.")
}
if secureBootEnabled {
    guard secureBootPKPath != nil else {
        fail("--secure-boot requires --secure-boot-pk <DER X.509 certificate path>.")
    }
    guard !secureBootDbEfiPaths.isEmpty || !secureBootDbHashes.isEmpty else {
        fail("--secure-boot requires at least one of --secure-boot-db-efi <PE path> or --secure-boot-db-hash <64-hex>.")
    }
    // M115c (#2462): enrollment overwrites the vars file every run, which
    // would destroy the profile's persistent efi-vars.bin — refuse the
    // combination rather than silently mangling a bundle member.
    guard activeProfile == nil else {
        fail("--secure-boot builds a FRESH variable store each run; --profile's efi-vars.bin is persistent. The combination is refused.")
    }
}

if !overlayMode {
    guard FileManager.default.fileExists(atPath: diskURL.path) else {
        fail("Disk image not found at '\(diskImagePath)'. Run 'zig build image' first.")
    }
}
try? FileManager.default.createDirectory(at: artifactsDir, withIntermediateDirectories: true)


try? FileManager.default.createDirectory(at: varsURL.deletingLastPathComponent(), withIntermediateDirectories: true)
let variableStore: VZEFIVariableStore
if secureBootEnabled {
    // M113b (#2418, ADR 0054 D5): a FRESH per-run store — enrollment is
    // one-shot setup, not accumulated state, so any existing file at this
    // path is deliberately overwritten.
    do {
        variableStore = try VZEFIVariableStore(creatingVariableStoreAt: varsURL, options: .allowOverwrite)
    } catch {
        fail("Could not create EFI variable store at \(varsURL.path): \(error)")
    }
#if SPIKE
    if #available(macOS 27.0, *) {
        guard let pkPath = secureBootPKPath,
              let der = FileManager.default.contents(atPath: pkPath),
              let platformKey = SecCertificateCreateWithData(nil, der as CFData) else {
            fail("--secure-boot-pk: cannot load a DER X.509 certificate from '\(secureBootPKPath ?? "")'.")
        }
        var signatures: [VZEFISignature] = []
        for pePath in secureBootDbEfiPaths {
            guard let pe = FileManager.default.contents(atPath: pePath) else {
                fail("--secure-boot-db-efi: cannot read '\(pePath)'.")
            }
            guard let digest = peAuthenticodeSHA256(pe) else {
                fail("--secure-boot-db-efi: '\(pePath)' is not a well-formed PE/COFF image.")
            }
            signatures.append(.sha256(digest))
            print("  secure-boot: db += authenticode(\(pePath)) = \(digest.map { String(format: "%02x", $0) }.joined())")
        }
        for hexDigest in secureBootDbHashes {
            guard let digest = dataFromHex(hexDigest), digest.count == 32 else {
                fail("--secure-boot-db-hash: '\(hexDigest)' is not 32 bytes of hex.")
            }
            signatures.append(.sha256(digest))
            print("  secure-boot: db += hash \(hexDigest)")
        }
        do {
            try variableStore.enableSecureBoot(platformKey: platformKey)
            let dbList = VZEFISignatureList(signatures: signatures)
            // ADR 0054 D1: custom PK + a db of the pinned digests only —
            // KEK and dbx stay empty, no Microsoft signatures are enrolled.
            let enrollment = VZEFISignatureDatabaseConfiguration(
                keyExchangeKeys: [],
                dbSignatures: [dbList],
                dbxSignatures: [])
            try variableStore.enrollSecureBootSignatures(enrollment)
            print("  secure-boot: enabled pk=\(pkPath) db_hashes=\(signatures.count) vars=\(varsURL.path) (fresh per-run store)")
        } catch {
            fail("--secure-boot: enrollment failed: \(error)")
        }
    } else {
        fail("--secure-boot requires macOS 27+ (VZEFIVariableStore Secure Boot APIs).")
    }
#else
    fail("--secure-boot requires a SPIKE build (macOS 27 SDK Secure Boot types); rebuild with `swift build --package-path host/vm-runner --configuration release -Xswiftc -DSPIKE` (`zig build spike-virtio` drives it). This binary cannot enroll Secure Boot signatures.")
#endif
} else if FileManager.default.fileExists(atPath: varsURL.path) {
    variableStore = VZEFIVariableStore(url: varsURL)
} else {
    do {
        variableStore = try VZEFIVariableStore(creatingVariableStoreAt: varsURL, options: .allowOverwrite)
    } catch {
        fail("Could not create EFI variable store at \(varsURL.path): \(error)")
    }
}

bootLoader.variableStore = variableStore
config.bootLoader = bootLoader
config.memorySize = UInt64(memoryMiB) * 1024 * 1024
config.cpuCount = cpuCount
// M70g G3 (#1459): a saved machine state is bound to the platform's
// machineIdentifier (VZGenericPlatformConfiguration.h: "When a virtual machine
// is saved to disk then loaded again, the machineIdentifier must match the
// machineIdentifier of the saved virtual machine"). VZ mints a random one per
// configuration, so the SAVE process persists its identifier and the LOAD
// process adopts it. Observed 2026-09-18 without this: restoreMachineStateFrom
// in a new process fails VZErrorRestore (12) "invalid argument". The default
// VM (and the same-process probe) keeps VZ's implicit platform, byte-identical.
if vzRestoreSaveDir != nil || vzRestoreLoadDir != nil {
    let platform = VZGenericPlatformConfiguration()
    if let load = vzRestoreLoadDir {
        let idURL = URL(fileURLWithPath: load, isDirectory: true).appendingPathComponent("machine-id.bin")
        guard let data = FileManager.default.contents(atPath: idURL.path),
              let id = VZGenericMachineIdentifier(dataRepresentation: data) else {
            fail("--vz-restore-load: no valid machine-id.bin in '\(load)' (run --vz-restore-save first).")
        }
        platform.machineIdentifier = id
        print("  platform: machineIdentifier adopted from \(idURL.path) (--vz-restore-load)")
    } else if let save = vzRestoreSaveDir {
        let dir = URL(fileURLWithPath: save, isDirectory: true)
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try platform.machineIdentifier.dataRepresentation.write(to: dir.appendingPathComponent("machine-id.bin"))
        } catch {
            fail("--vz-restore-save: could not persist the machine identifier into '\(save)': \(error)")
        }
        print("  platform: machineIdentifier persisted to \(dir.appendingPathComponent("machine-id.bin").path) (--vz-restore-save)")
    }
    config.platform = platform
} else if let p = activeProfile {
    // M115c (#2462): a profiled machine keeps ONE machineIdentifier for its
    // whole life — adopt the bundle's machine-id.bin (minted at create,
    // reminted+persisted here if absent or unreadable by VZ) so a later
    // saved state binds and every boot presents the same machine.
    let platform = VZGenericPlatformConfiguration()
    if let data = p.machineIdentifierData(),
       let id = VZGenericMachineIdentifier(dataRepresentation: data) {
        platform.machineIdentifier = id
    } else {
        do {
            try p.setMachineIdentifier(platform.machineIdentifier.dataRepresentation)
        } catch {
            fail("--profile: could not persist \(p.machineIdentifierURL.path): \(error)")
        }
        print("  platform: machineIdentifier minted into \(p.machineIdentifierURL.path)")
    }
    config.platform = platform
}

do {
    if let base = overlayBasePath {
#if canImport(DiskImageKit)
        guard #available(macOS 27.0, *) else {
            fail("--overlay-base requires macOS 27+ DiskImageKit.")
        }
        // macOS 27 DiskImageKit stacked image (issue #523 item 2, claim
        // 6637): read-only base + a fresh ASIF overlay layer in a private
        // temp dir (removed at exit by the atexit handler). API verified
        // against the Xcode 27 SDK swiftinterfaces.
        let baseURL = URL(fileURLWithPath: base)
        guard FileManager.default.fileExists(atPath: baseURL.path) else {
            fail("Overlay base image not found at '\(base)'.")
        }
        let diskBase = try DiskImage(opening: .open(url: baseURL, mode: .readOnly))
        if let load = vzRestoreLoadDir {
            // M70g G3 (#1459): the LOAD process must see the disk exactly as
            // the SAVE process left it, so it re-opens the overlay that
            // process persisted instead of creating a fresh one.
            let overlayURL = URL(fileURLWithPath: load, isDirectory: true).appendingPathComponent("overlay.asif")
            guard FileManager.default.fileExists(atPath: overlayURL.path) else {
                fail("--vz-restore-load: no overlay.asif in '\(load)' (run --vz-restore-save first).")
            }
            let overlay = try DiskImage(opening: .open(url: overlayURL, mode: .readWrite))
            let stacked = try diskBase.appending(overlay)
            let attachment = try VZDiskImageStorageDeviceAttachment(diskImage: stacked)
            config.storageDevices = [VZVirtioBlockDeviceConfiguration(attachment: attachment)]
            print("  disk: \(base) (base, read-only) + persisted ASIF overlay \(overlayURL.path) (--vz-restore-load)")
        } else {
            let stackDir: URL
            if let save = vzRestoreSaveDir {
                // M70g G3 (#1459): the overlay outlives this process — no
                // cleanup; the load process (or a failed run's post-mortem)
                // owns it.
                stackDir = URL(fileURLWithPath: save, isDirectory: true)
            } else if let p = activeProfile {
                // M115c (#2462): the overlay lives INSIDE the bundle — a
                // persisted member, never the throwaway tmp layer, and never
                // the atexit cleanup.
                stackDir = p.url
            } else {
                stackDir = FileManager.default.temporaryDirectory
                    .appendingPathComponent("virelai-overlay-\(UUID().uuidString)", isDirectory: true)
                overlayCleanupPath = stackDir.path
            }
            try FileManager.default.createDirectory(at: stackDir, withIntermediateDirectories: true)
            let overlayURL = stackDir.appendingPathComponent("overlay.asif")
            let stacked: any StackedImage
            if let p = activeProfile, FileManager.default.fileExists(atPath: overlayURL.path) {
                // M115c: a profiled machine REOPENS its persisted overlay —
                // guest writes to the boot disk survive across sessions.
                // (The vz-restore-save leg above is unreachable with a
                // profile: --profile is not in that probe's flag set.)
                _ = p
                let overlay = try DiskImage(opening: .open(url: overlayURL, mode: .readWrite))
                stacked = try diskBase.appending(overlay)
            } else {
                let layer = ASIFCreationConfiguration.layer(
                    url: overlayURL,
                    type: .overlay(blockCount: Int(diskBase.blockCount)))
                stacked = try diskBase.appending(layer)
            }
            let attachment = try VZDiskImageStorageDeviceAttachment(diskImage: stacked)
            config.storageDevices = [VZVirtioBlockDeviceConfiguration(attachment: attachment)]
            if vzRestoreSaveDir != nil {
                print("  disk: \(base) (base, read-only) + persisted ASIF overlay in \(stackDir.path) (--vz-restore-save; kept for the load process)")
            } else if activeProfile != nil {
                print("  disk: \(base) (base, read-only) + persistent ASIF overlay \(overlayURL.path) (--profile)")
            } else {
                print("  disk: \(base) (base, read-only) + throwaway ASIF overlay in \(stackDir.path)")
            }
        }
#else
        fail("--overlay-base needs an SDK with DiskImageKit (macOS 27+); this binary was built without it.")
#endif
    } else {
        let attachment = try VZDiskImageStorageDeviceAttachment(url: diskURL, readOnly: false)
        config.storageDevices = [VZVirtioBlockDeviceConfiguration(attachment: attachment)]
    }
} catch {
    fail("Could not attach disk image '\(diskImagePath)': \(error)")
}

// M100b (issue #2307, ADR 0050 D1/D2): --data-disk appends a SECOND
// VZVirtioBlockDeviceConfiguration in every attach mode (plain and
// --overlay-base — the overlay covers the boot disk only; the data disk
// itself is never stacked). It is named "virelai-vlfs" so the guest's
// VIRTIO_BLK_T_GET_ID can tell it from the boot image.
// Issue #3112: the image reaches the guest through a kernel block
// device + VZDiskBlockDeviceStorageDeviceAttachment rather than a
// file-backed attachment — see the attach site below.
// A missing file becomes a zero-filled raw image (default 64 MiB); the
// guest then refuses mount with not_vlfs until mkfs.vlfs (M100c)
// formats it — never the runner's or kernel's job.
if let dataDiskPath {
    do {
        let prepared = try DataDisk.prepare(path: dataDiskPath)
        try VZVirtioBlockDeviceConfiguration.validateBlockDeviceIdentifier(DataDisk.vlfsIdentifier)
        // Issue #3112: the image is attached as a kernel block device
        // (/dev/diskN via `diskutil image attach`) and handed to VZ's
        // block-device attachment — VZ's file-backed attachment persists
        // guest writes on an asynchronous pipeline, and a hard vm.stop
        // stranded requests the guest had already completed (and read
        // back byte-exact), leaving the image behind the guest-coherent
        // view at sector granularity (live-vlfs-crash sealed legs 06/07
        // flagged bad_block_auth on those homes). Through the kernel
        // driver a completed guest write is in the file — ordered —
        // before the power drops, and the guest gets a real flush
        // barrier (the NBD attachment was ruled out: VZ's NBD client
        // never advertises VIRTIO_BLK_F_FLUSH, so vlfs's commit point
        // can never complete — verified against qemu-nbd).
        let dev = try DataDisk.attachBlockDevice(url: prepared.url)
        dataDiskBlockDev = dev
        // NOTE: deliberately no atexit eject — the disk-image driver's
        // own pipeline drains asynchronously past vm.stop, and ejecting
        // while a descriptor is still open (or racing the drain) drops
        // completed writes. A stale node is self-healing: the next
        // attach of the same image returns it.
        let fh = try FileHandle(forUpdating: URL(fileURLWithPath: DataDisk.rawDevice(dev)))
        dataDiskBlockFH = fh
        let attachment = try VZDiskBlockDeviceStorageDeviceAttachment(
            fileHandle: fh, readOnly: false, synchronizationMode: .full)
        let dataDisk = VZVirtioBlockDeviceConfiguration(attachment: attachment)
        dataDisk.blockDeviceIdentifier = DataDisk.vlfsIdentifier
        config.storageDevices.append(dataDisk)
        if prepared.created {
            print("  data-disk: created zero-filled raw image \(dataDiskPath) (\(prepared.sectors) sectors)")
        } else {
            print("  data-disk: \(dataDiskPath) (\(prepared.sectors) sectors)")
        }
    } catch {
        fail("Could not attach --data-disk image '\(dataDiskPath)': \(error)")
    }
}

// ---------------------------------------------------------------------------
// Serial plumbing: evidence path (unchanged) vs console path (duplex + tee).
// ---------------------------------------------------------------------------


FileManager.default.createFile(atPath: serialURL.path, contents: nil)
if consoleMode || scriptMode {
    do {
        serialLogHandle = try FileHandle(forWritingTo: serialURL)
    } catch {
        fail("Could not open serial log at \(serialURL.path): \(error)")
    }
}

if let path = cvcConsoleFilePath {
    let url = URL(fileURLWithPath: path)
    FileManager.default.createFile(atPath: url.path, contents: nil)
    do {
        cvcConsoleFileHandle = try FileHandle(forWritingTo: url)
    } catch {
        fail("Could not open cvc console file at \(url.path): \(error)")
    }
}

if consoleMode || scriptMode {
    // Duplex attachment: the host-to-guest input handle is non-nil.
    serialConfig.attachment = VZFileHandleSerialPortAttachment(
        fileHandleForReading: consoleInputPipe.fileHandleForReading,
        fileHandleForWriting: consoleOutputPipe.fileHandleForWriting
    )
} else {
    // Evidence path: output-only attachment, exactly as before.
    let serialHandle: FileHandle
    do {
        serialHandle = try FileHandle(forWritingTo: serialURL)
    } catch {
        fail("Could not open serial log at \(serialURL.path): \(error)")
    }
    serialConfig.attachment = VZFileHandleSerialPortAttachment(
        // M114d (#2457): --host-session gets the read end too — the
        // drawer's injector (send-serial) needs a guest RX path; vzRestore
        // already needed it for the probe's serial query.
        fileHandleForReading: (vzRestore || hostSessionMode) ? consoleInputPipe.fileHandleForReading : nil,
        fileHandleForWriting: serialHandle
    )
}
config.serialPorts = [serialConfig]
// M97g (#2084): --no-entropy leaves entropyDevices empty — the guest sees
// no DID 0x1044 device, the boot seed fails, and slot 72 must refuse.
config.entropyDevices = noEntropy ? [] : [VZVirtioEntropyDeviceConfiguration()]
// M183b (issue #3172): --balloon attaches ONE
// VZVirtioTraditionalMemoryBalloonDeviceConfiguration (VZ allows exactly
// one per VM). OFF by default — the flags-off configuration stays
// byte-identical. The device is the host-reclaim mechanism: the runner's
// --balloon-target-after triggers write targetVirtualMachineMemorySize,
// and the guest's virtio_balloon driver inflates/deflates toward the
// resulting num_pages on its polled idle pass.
if balloonEnabled {
    config.memoryBalloonDevices = [VZVirtioTraditionalMemoryBalloonDeviceConfiguration()]
}

// Milestone six card G1 (claim 6053): the virtio-gpu device is attached
// under `--display` (the milestone's mode — the machine boots to a
// screen) as well as under `--screenshot` (the evidence capture). The
// default VM attaches NO graphics device — config.graphicsDevices stays
// [] and every existing gate stays byte-identical.
if screenshotPath != nil || displayMode {
    let graphics = VZVirtioGraphicsDeviceConfiguration()
    graphics.scanouts = [VZVirtioGraphicsScanoutConfiguration(widthInPixels: 1280, heightInPixels: 720)]
    config.graphicsDevices = [graphics]
} else {
    config.graphicsDevices = []
}
// Milestone seven card I1 (claim 4272; premise corrected by claim 3868):
// the keyboard + pointing devices are attached only under `--input`. The
// default VM attaches none — config.keyboards/pointingDevices stay [] and
// every existing gate stays byte-identical. The guest observes an Apple
// XHCI USB controller (VID=0x106b DID=0x1a06), NOT virtio-input — the
// claim-3868 finding.
if inputMode {
    config.keyboards = [VZUSBKeyboardConfiguration()]
    config.pointingDevices = [VZUSBScreenCoordinatePointingDeviceConfiguration()]
} else {
    config.keyboards = []
    config.pointingDevices = []
}
// M43 card U6: the USB mass storage device rides an explicit XHCI controller
// configuration (the macOS 15+ usbControllers surface). Purely additive —
// without --usb-msd the array stays empty and the boot is byte-identical.
if let usbMsdPath {
    let usbMsdURL = URL(fileURLWithPath: usbMsdPath)
    guard FileManager.default.fileExists(atPath: usbMsdURL.path) else {
        fail("--usb-msd: disk image not found: \(usbMsdPath)")
    }
    do {
        let msdAttachment = try VZDiskImageStorageDeviceAttachment(url: usbMsdURL, readOnly: false)
        let msd = VZUSBMassStorageDeviceConfiguration(attachment: msdAttachment)
        let xhciController = VZXHCIControllerConfiguration()
        xhciController.usbDevices = [msd]
        config.usbControllers = [xhciController]
    } catch {
        fail("--usb-msd: could not attach disk image \(usbMsdPath): \(error)")
    }
}
if netInjectPath != nil, netCapturePath == nil {
    fail("--net-inject requires --net (the injection writes into the SAME attachment's socket).")
}
if netArpRespondHostIP != nil, netCapturePath == nil {
    fail("--net-arp-respond requires --net (the ARP reply is written into the SAME attachment's socket).")
}
if netRaRespondPrefix != nil, netCapturePath == nil {
    fail("--net-ra-respond requires --net (the RA is written into the SAME attachment's socket).")
}
if !netNdpRespondAddrs.isEmpty, netCapturePath == nil {
    fail("--net-ndp-respond requires --net (the NA is written into the SAME attachment's socket).")
}
if netIcmp6RespondAddr != nil, netCapturePath == nil {
    fail("--net-icmp6-respond requires --net (the echo reply is written into the SAME attachment's socket).")
}
if netUdp6RespondAddr != nil, netCapturePath == nil {
    fail("--net-udp6-respond requires --net (the UDP reply is written into the SAME attachment's socket).")
}
if netTcp6RespondAddr != nil, netCapturePath == nil {
    fail("--net-tcp6-respond requires --net (the TCP replies are written into the SAME attachment's socket).")
}
if netIcmpRespondHostIP != nil, netCapturePath == nil {
    fail("--net-icmp-respond requires --net (the ICMP reply is written into the SAME attachment's socket).")
}
if netUdpRespondHostIP != nil, netCapturePath == nil {
    fail("--net-udp-respond requires --net (the UDP reply is written into the SAME attachment's socket).")
}
if netDnsRespondHostIP != nil, netCapturePath == nil {
    fail("--net-dns-respond requires --net (the DNS reply is written into the SAME attachment's socket).")
}
// M99i (issue #2187): the depth knobs are meaningless without the
// responder they configure.
if netDnsRespondHostIP == nil,
   !netDnsAnswers.isEmpty || !netDnsAnswers6.isEmpty || netDnsTTL != 300 ||
   netDnsSpoof || netDnsWrongQuestion || netDnsTruncate {
    fail("--net-dns-answer/--net-dns-answer6/--net-dns-ttl/--net-dns-spoof/--net-dns-wrong-question/--net-dns-truncate require --net-dns-respond.")
}
if netSntpRespondHostIP != nil, netCapturePath == nil {
    fail("--net-sntp-respond requires --net (the SNTP reply is written into the SAME attachment's socket).")
}
if netSntpSkew != nil, netSntpRespondHostIP == nil {
    fail("--net-sntp-skew requires --net-sntp-respond.")
}
if netDhcpRespondLeaseIP != nil, netCapturePath == nil {
    fail("--net-dhcp-respond requires --net (the DHCP reply is written into the SAME attachment's socket).")
}
if netTcpRespondHostIP != nil, netCapturePath == nil {
    fail("--net-tcp-respond requires --net (the TCP reply is written into the SAME attachment's socket).")
}
// M51 SSH1 (#1168): the paced packet mode needs a payload to send.
if netTcpRespondPacketMode, netTcpRespondPacketPayload.isEmpty {
    fail("--net-tcp-respond :packet requires --net-tcp-respond-payload <file>.")
}
// M51 SSH5 (#1172): the SSH responder's hand-rolled cipher must reproduce
// the OpenSSH `PROTOCOL.chacha20poly1305` vector the guest's Zig
// implementation pins (ADR 0023 D2 drift guard) before the mode is armed.
if netTcpRespondSSHMode {
    guard SSHFixtures.cipherSelfCheck() else {
        fail("--net-tcp-respond :ssh cipher self-check FAILED (the OpenSSH PROTOCOL.chacha20poly1305 pinned vector did not reproduce). Refusing to serve the SSH responder.")
    }
    print("NET-TCP-SSH: cipher self-check ok (OpenSSH PROTOCOL.chacha20poly1305 pinned vector, seq 7, tag \(SSHFixtures.opensshVectorTagHex))")
}
// M51 interop (#1209): `:relay` needs the real upstream to forward to.
if netTcpRespondRelayMode, netTcpRespondRelayPort == nil {
    fail("--net-tcp-respond :relay requires --net-tcp-respond-relay <host-ip>:<host-port> (the real upstream TCP server).")
}
if netTcpConnectGuestIP != nil, netCapturePath == nil {
    fail("--net-tcp-connect requires --net (the client frames are written into the SAME attachment's socket).")
}
if nativeSocketProbeMode != nil && (netCapturePath == nil || netNatEnabled || netTcpConnectGuestIP != nil) {
    fail("--net-native-preview requires hermetic --net, no NAT or other connecting peer")
}
if netTcpConnectStreamExec != nil && (netTcpClients.isEmpty || netTcpClients.first!.sshMode || !netTcpClients.first!.payload.isEmpty || netNatEnabled) {
    fail("--net-tcp-connect-stream requires --net-tcp-connect <ip>:<port> with --net; no SSH, payload, or NAT")
}
if netTcpConnectStreamExec == nil && (!netTcpConnectStreamArgs.isEmpty || netTcpConnectIntruder) {
    fail("--net-tcp-connect-stream-arg and --net-tcp-connect-intruder require --net-tcp-connect-stream")
}
if netTcpConnectSSHPty != nil || netTcpConnectSSHKeys != nil || netTcpConnectSSHSessionFile != nil {
    guard netTcpConnectSSHMode else {
        fail("--net-tcp-connect-ssh-pty/-ssh-keys/-ssh-session-file require a :ssh --net-tcp-connect target.")
    }
}
if netTcpConnectSSHKeys != nil && netTcpConnectSSHPty == nil {
    fail("--net-tcp-connect-ssh-keys requires --net-tcp-connect-ssh-pty <cols>x<rows> (the script drives a live shell).")
}
if netTcpConnectSSHMode {
    guard SSHFixtures.cipherSelfCheck() else {
        fail("--net-tcp-connect :ssh cipher self-check FAILED (the OpenSSH PROTOCOL.chacha20poly1305 pinned vector did not reproduce). Refusing to arm the SSH client.")
    }
    print("NET-TCP-CONNECT-SSH: cipher self-check ok (OpenSSH PROTOCOL.chacha20poly1305 pinned vector, seq 7, tag \(SSHFixtures.opensshVectorTagHex))")
}
// M206a (#3813, ADR 0057): `--net-hub` rides the --net attachment — the
// hub relays the same socketpair's guest TX datagrams and writes its
// deliveries back the host->guest way. It composes with --net (the
// capture stays byte-exact) and refuses by name alongside every other
// attachment and alongside --net-dhcp-respond (the hub owns DHCP on the
// segment — one lease per source MAC).
if netHubPath != nil, netCapturePath == nil {
    fail("--net-hub requires --net <capture-file> (the hub relays the same attachment's datagrams).")
}
if netHubPath != nil, netNatEnabled {
    fail("--net-hub is mutually exclusive with --net-nat (one network device per guest for now).")
}
if netHubPath != nil, netBridgedInterface != nil {
    fail("--net-hub is mutually exclusive with --net-bridged (one network device per guest for now).")
}
if netHubPath != nil, netVmnetEnabled {
    fail("--net-hub is mutually exclusive with --net-vmnet (one network device per guest for now).")
}
if netHubPath != nil, netDhcpRespondLeaseIP != nil {
    fail("--net-hub is mutually exclusive with --net-dhcp-respond (the hub owns DHCP on the segment — one lease per MAC).")
}
if netMacOverride != nil, netCapturePath == nil, !netNatEnabled, netBridgedInterface == nil, !netVmnetEnabled {
    fail("--mac requires a network attachment (--net, --net-nat, --net-bridged or --net-vmnet).")
}
// Milestone five card N7 (claim 4678): `--net-nat` is mutually exclusive
// with `--net` — one network device per guest for now (the flag
// validation shape: a clear fail, like the responder requirements above).
if netNatEnabled, netCapturePath != nil {
    fail("--net-nat is mutually exclusive with --net (one network device per guest for now).")
}
if netBridgedInterface != nil {
    if netCapturePath != nil {
        fail("--net-bridged is mutually exclusive with --net (one network device per guest for now).")
    }
    if netNatEnabled {
        fail("--net-bridged is mutually exclusive with --net-nat (one network device per guest for now).")
    }
    if netVmnetEnabled {
        fail("--net-bridged is mutually exclusive with --net-vmnet (one network device per guest for now).")
    }
    let wanted = netBridgedInterface!
    guard let iface = VZBridgedNetworkInterface.networkInterfaces.first(where: { $0.identifier == wanted }) else {
        let available = VZBridgedNetworkInterface.networkInterfaces.map { $0.identifier }.joined(separator: ",")
        fail("--net-bridged: interface '\(wanted)' is not in VZBridgedNetworkInterface.networkInterfaces (bridgeable here: \(available.isEmpty ? "<none>" : available)).")
    }
    if !runnerHasEntitlement("com.apple.vm.networking") {
        fail("--net-bridged requires the com.apple.vm.networking entitlement, which this binary's signature does not carry. It is a RESTRICTED entitlement on macOS 27: an ad-hoc codesign (--sign -) carrying it is killed by amfid before main() (observed). Sign with a development identity using host/vm-runner/entitlements-net.plist, or use --net/--net-nat instead.")
    }
    netBridgedIface = iface
}
if netVmnetEnabled {
    if netCapturePath != nil {
        fail("--net-vmnet is mutually exclusive with --net (one network device per guest for now).")
    }
    if netNatEnabled {
        fail("--net-vmnet is mutually exclusive with --net-nat (one network device per guest for now).")
    }
    if netBridgedInterface != nil {
        fail("--net-vmnet is mutually exclusive with --net-bridged (one network device per guest for now).")
    }
#if canImport(vmnet)
    var netVmnetStatus = vmnet_return_t.VMNET_SUCCESS
    guard let vmnetConfig = vmnet_network_configuration_create(operating_modes_t.VMNET_SHARED_MODE, &netVmnetStatus) else {
        fail("--net-vmnet: vmnet_network_configuration_create failed (vmnet_return_t=\(netVmnetStatus.rawValue) \(vmnetStatusName(netVmnetStatus))).")
    }
    if let subnet = netVmnetSubnet {
        // The daemon's set_ipv4_subnet takes the GATEWAY address — the
        // second address of the range — not the base (observed in
        // apple/containerization's configureSubnetV4, and by the daemon's
        // own refusal: passing the base gives "can't have the gateway at
        // the subnet address" at interface start, err 49).
        let mv: UInt32 = ~UInt32(0) << (32 - subnet.prefixLen)
        let av = UInt32(subnet.ip[0]) << 24 | UInt32(subnet.ip[1]) << 16 | UInt32(subnet.ip[2]) << 8 | UInt32(subnet.ip[3])
        var saddr = in_addr(s_addr: (av + 1).byteSwapped)
        var smask = in_addr(s_addr: mv.byteSwapped)
        let setStatus = vmnet_network_configuration_set_ipv4_subnet(vmnetConfig, &saddr, &smask)
        guard setStatus == .VMNET_SUCCESS else {
            fail("--net-vmnet-subnet: vmnet_network_configuration_set_ipv4_subnet failed (vmnet_return_t=\(setStatus.rawValue) \(vmnetStatusName(setStatus))).")
        }
    }
    var netVmnetCreateStatus = vmnet_return_t.VMNET_SUCCESS
    guard let network = vmnet_network_create(vmnetConfig, &netVmnetCreateStatus) else {
        fail("--net-vmnet: vmnet_network_create failed (vmnet_return_t=\(netVmnetCreateStatus.rawValue) \(vmnetStatusName(netVmnetCreateStatus))) — the vmnet daemon requires the com.apple.developer.networking.vmnet entitlement or root (observed in the daemon's client_entitlement_check). The entitlement is restricted: an ad-hoc codesign cannot carry it — sign with a development identity using host/vm-runner/entitlements-net.plist.")
    }
    netVmnetNetwork = network
    var netSub = in_addr(), netMask = in_addr()
    vmnet_network_get_ipv4_subnet(network, &netSub, &netMask)
    netVmnetSubnetText = "\(ipv4Text(netSub))/\(prefixLen(netMask))"
#else
    fail("--net-vmnet requires a macOS 26+ SDK build (vmnet_network_create); this binary was compiled without vmnet.framework.")
#endif
}
if netVmnetSubnet != nil, !netVmnetEnabled {
    fail("--net-vmnet-subnet requires --net-vmnet.")
}
// M70g G2 (#1459): the bridge secret comes from the file OR the inline spec,
// never both; the file form is preferred because argv is world-readable.
if let fileSecret = consoleTCPSecretFromFile {
    guard consoleTCPPort != nil else {
        fail("--console-tcp-secret-file requires --console-tcp.")
    }
    guard consoleTCPSecret == nil else {
        fail("--console-tcp-secret-file: the --console-tcp spec already carries an inline secret; use one or the other.")
    }
    consoleTCPSecret = fileSecret
} else if consoleTCPSecret != nil {
    FileHandle.standardError.write(Data("WARNING: --console-tcp: an inline secret is visible in the process list; prefer --console-tcp-secret-file <path>\n".utf8))
}
// Card G6 set_visible follow-on (claim 0487): the marker-driven capture
// writes into the `--screen <base>` filename, so it requires the flag.
if screenshotAfter != nil, screenshotPath == nil {
    fail("--screenshot-after requires --screen (the marker capture writes into the --screen base filename).")
}
// M81f (#1766): a host-side share mutation only means anything when the
// guest is actually reading that share over the file channel, and the three
// flags form ONE trigger — a half-specified one is a parse error, not a
// silent no-op.
if hostWriteAfter != nil || hostWritePath != nil || hostWriteContent != nil {
    guard hostWriteAfter != nil, hostWritePath != nil else {
        fail("--host-write/--host-write-after/--host-write-content form one marker-anchored mutation: give --host-write <relpath> and --host-write-after <marker> (--host-write-content is the optional body).")
    }
    guard cvcFileEnabled else {
        fail("--host-write-after requires --cvc-file <share-dir> (the mutation must land in the share the guest reads).")
    }
}
// M108b (#2325, ADR 0051 D6): the lane flags are modifiers on --cvc-file —
// the share IS the transport, so a lane flag without it is a fatal argument
// error (loud, never a silent ignore — the --via-virtio rule).
if hostClipboardFlag || hostPasteboardName != nil || hostClipInText != nil || hostClipInAfter != nil || hostClipInFile != nil || hostClipInMime != nil {
    guard hostClipboardFlag else {
        fail("--host-pasteboard/--host-clip-in/--host-clip-in-after are modifiers on --host-clipboard (no lane to redirect or inject into).")
    }
    guard cvcFileEnabled else {
        fail("--host-clipboard requires --cvc-file <share-dir> (the lanes are signed files in that share — ADR 0051 D1).")
    }
    if hostClipInAfter != nil, hostClipInText == nil, hostClipInFile == nil {
        fail("--host-clip-in-after requires --host-clip-in <text> or --host-clip-in-file <path> (there is no pasteboard write to schedule).")
    }
    if hostClipInFile != nil, hostClipInMime == nil {
        fail("--host-clip-in-file requires --host-clip-in-mime <mime> (the type the inject negotiates on the pasteboard).")
    }
    if hostClipInMime != nil, hostClipInFile == nil {
        fail("--host-clip-in-mime requires --host-clip-in-file <path> (mime alone has no bytes to offer).")
    }
    if hostClipInText != nil || hostClipInFile != nil, hostClipInAfter == nil {
        // Default trigger: fire as soon as the seat announces the lane is
        // armed — the deterministic point where a CLIP_IN can be consumed.
        hostClipInAfter = "gotabwm: hostint armed"
    }
}
// M108c (#2326, ADR 0051 D6): the drop lane is the same shape — a modifier
// on --cvc-file, its hooks modifiers on the lane.
if hostDropFlag || !hostDropFiles.isEmpty || hostDropAfter != nil {
    guard hostDropFlag else {
        fail("--host-drop-file/--host-drop-after are modifiers on --host-drop (no lane to inject into).")
    }
    guard cvcFileEnabled else {
        fail("--host-drop requires --cvc-file <share-dir> (the lanes are signed files in that share — ADR 0051 D1).")
    }
    if hostDropAfter != nil, hostDropFiles.isEmpty {
        fail("--host-drop-after requires --host-drop-file <path> (there is no drop to schedule).")
    }
    if !hostDropFiles.isEmpty, hostDropAfter == nil {
        // Default trigger, same shape as --host-clip-in: fire once the
        // seat has adopted the lane's epoch.
        hostDropAfter = "gotabwm: hostint armed"
    }
}
// M108d (#2328, ADR 0051 D6): notify-out is the same modifier shape — no
// display prerequisite (banners do not need the VM window).
if hostNotifyFlag {
    guard cvcFileEnabled else {
        fail("--host-notify requires --cvc-file <share-dir> (the lanes are signed files in that share — ADR 0051 D1).")
    }
}
// M127d (#2602, ADR 0051 V2-D5): the outbox lane is the same modifier
// shape — no display prerequisite (exports are not a window gesture).
if hostOutboxFlag || hostOutboxDir != nil {
    guard hostOutboxFlag else {
        fail("--host-outbox-dir is a modifier on --host-outbox (no lane to redirect).")
    }
    guard cvcFileEnabled else {
        fail("--host-outbox requires --cvc-file <share-dir> (the lanes are signed files in that share — ADR 0051 D1).")
    }
}
// M186d (#3181, ADR 0051 V2-D8): the speech lane is the same modifier
// shape — no display prerequisite (speech is not a window gesture).
if hostSpeechFlag {
    guard cvcFileEnabled else {
        fail("--host-speech requires --cvc-file <share-dir> (the lanes are signed files in that share — ADR 0051 D1).")
    }
}
// M202c (#3386): the print lane is the same modifier shape — no display
// prerequisite (the bare runner's degrade is a file landing, not a window
// gesture).
if hostPrintFlag || hostPrintDir != nil {
    guard hostPrintFlag else {
        fail("--host-print-dir is a modifier on --host-print (no lane to redirect).")
    }
    guard cvcFileEnabled else {
        fail("--host-print requires --cvc-file <share-dir> (the lanes are signed files in that share — ADR 0051 D1).")
    }
}
// Claim 9367: --pointer-virtio rides the four-queue device; the after-marker
// only makes sense with a sequence to schedule.
if pointerVirtioScript == nil, pointerVirtioAfter != nil {
    fail("--pointer-virtio-after requires --pointer-virtio (there is no sequence to schedule).")
}
// Claim 0680: snapshot requests ride the five-queue device and stream the
// composed scanout, which only exists when the GPU is attached.
if !snapshotAfterMarkers.isEmpty, screenshotPath == nil {
    fail("--snapshot-after requires --screen (the guest composites into the virtio-gpu scanout; without it there is no framebuffer to stream).")
}
if cvcConsoleFilePath != nil, !customVirtioEnabled {
    fail("--cvc-console-file requires the custom virtio device (queue 1 carries the structured console).")
}
// M102d3 (#2168): the opt-in render device and its SPIKE-only fault knob
// are validated BEFORE any device is configured — every refusal is named,
// never a silent fallback (the leaf also covers the ordinary-build case:
// the flag parses, then attachment refuses by name).
if let rejection = GPU3DRunnerConfiguration.rejection(for: gpu3dOptions) {
    fail("\(rejection)")
}
if let sharePath = virtioFSShareDir {
    let shareURL = URL(fileURLWithPath: sharePath, isDirectory: true)
    var isDirectory: ObjCBool = false
    guard FileManager.default.fileExists(atPath: shareURL.path, isDirectory: &isDirectory),
          isDirectory.boolValue else {
        fail("--virtio-fs requires an existing host directory, got '\(sharePath)'.")
    }
    let fileSystem = VZVirtioFileSystemDeviceConfiguration(tag: "virelaios")
    fileSystem.share = VZSingleDirectoryShare(
        directory: VZSharedDirectory(url: shareURL, readOnly: false)
    )
    config.directorySharingDevices = [fileSystem]
    print("VIRTIO-FS: ENABLED tag=virelaios share=\(shareURL.path) read-write")
}
// M115a (#2460): Apple's standard virtio-socket device — the candidate
// restore-safe guest↔host channel (the custom-virtio device is refused by
// validateSaveRestoreSupport; this probe measures whether the SDK's own
// vsock device is). Attached only under --vsock; the host listener on
// --vsock-port is armed on the live device after the VM object exists.
if vsockEnabled {
    config.socketDevices = [VZVirtioSocketDeviceConfiguration()]
    print("VSOCK: ENABLED host-listen-port=\(vsockPort)")
}
// Claim 0680: build the trigger list — one numbered raw file per
// --snapshot-after instance, in registration order.
do {
    let snapBase = snapshotOutBase ?? (screenshotPath.map { $0 + "-snap" } ?? "snapshot")
    for (i, marker) in snapshotAfterMarkers.enumerated() {
        snapshotTriggers.append(SnapshotTrigger(marker: marker, outPath: "\(snapBase)-\(i).raw"))
    }
}

if let netCapturePath {
    let netURL = URL(fileURLWithPath: netCapturePath)
    FileManager.default.createFile(atPath: netURL.path, contents: nil)
    guard let netCaptureFile = try? FileHandle(forWritingTo: netURL) else {
        fail("Could not open net capture file at '\(netCapturePath)'.")
    }
    netCaptureHandle = netCaptureFile
    var fds: [Int32] = [0, 0]
    guard socketpair(AF_UNIX, SOCK_DGRAM, 0, &fds) == 0 else {
        fail("Could not create the net attachment datagram socketpair (errno \(errno)).")
    }
    netCaptureSocket = FileHandle(fileDescriptor: fds[0], closeOnDealloc: true)
    netCaptureReadSocket = FileHandle(fileDescriptor: fds[1], closeOnDealloc: true)
    // Host->guest depth is NOT a socket option: deepening this pair's
    // SO_RCVBUF broke VZ delivery on the live gate (a guest got exactly
    // one frame, then none). The deep queue is NetGuestQueue (userspace)
    // — every writer below and the hub's selfPort deliveries ride it.
    // M206a (#3813): attach the hub BEFORE the capture thread so the
    // relay exists for the guest's first TX. First-runner-binds: a live
    // hub at <path> makes this runner a client; a stale file (connect()
    // refusing — observed) is unlinked and retaken as owner. The RX
    // thread writes hub deliveries into the runner's socket end — the
    // host->guest direction, so the capture file stays guest-TX-only.
    if let hubPath = netHubPath {
        do {
            netHub = try NetHub.attach(path: hubPath)
        } catch let err as NetHubError {
            fail("--net-hub '\(hubPath)': \(err.message).")
        } catch {
            fail("--net-hub '\(hubPath)': \(error).")
        }
        netHub?.startRx()
        atexit { netHub?.unlinkBoundPaths() }
        print("NET-HUB: \(netHub!.role == .owner ? "bound hub socket \(hubPath) — this runner owns the segment hub" : "attached to hub \(hubPath) as client \(netHub!.boundPath)")")
    }
    // Reader thread: drain the runner's socket end into the capture file.
    // One datagram per read (SOCK_DGRAM); a 4096-byte buffer covers the
    // largest N1 frame (1514 B) with headroom.
    netCaptureThread = Thread {
        // Card N3 (claim 7293): the host-side ARP responder's fixed MAC —
        // the same locally-administered address as the guest's
        // fallback_mac, so the reply the guest learns is deterministic and
        // gate-assertable.
        let arpHostMAC: [UInt8] = [0x02, 0x00, 0x00, 0x00, 0x00, 0x02]
        var buf = [UInt8](repeating: 0, count: 4096)
        while !netCaptureStop {
            let n = read(netCaptureReadSocket!.fileDescriptor, &buf, buf.count)
            if n < 0 && errno == EINTR { continue }
            if n <= 0 { break }
            try? netCaptureFile.write(contentsOf: Data(bytes: buf, count: n))
            // M206a (#3813): the capture above is byte-exact and FIRST;
            // the hub relay then carries the same datagram to the
            // segment (owner: in-process switch; client: sendto the hub
            // path). Hub->guest frames travel the other socket direction
            // and never touch the capture.
            netHub?.guestTx(Array(buf[0..<n]))
            // Guest TX implies the peer is draining: also flush the
            // host->guest queue so queued frames don't wait on the rx
            // thread's 250 ms wake.
            netGuestQueue.flush()
            // Card N3: if the guest asked an ARP request, answer it from
            // the host. The reply is written into the runner's socket end
            // (fds[1]) — the card-N2 host→guest direction (VZ reads
            // fds[0]), so the guest receives it; the capture file above
            // keeps only guest TX (byte-exact, unchanged).
            if let hostIP = netArpRespondHostIP, isArpRequest(buf, n) {
                var reply = [UInt8](repeating: 0, count: 42)
                buildArpReply(&reply, buf, n, arpHostMAC, hostIP)
                netGuestQueue.send(reply)
                print("NET-ARP: answered the guest's ARP request for \(buf[28]).\(buf[29]).\(buf[30]).\(buf[31]) with host MAC 02:00:00:00:00:02")
            }
            // Card N4: if the guest pinged our address, echo it back from
            // the host (same socket direction; the capture file above
            // keeps only guest TX, byte-exact, unchanged).
            if let hostIP = netIcmpRespondHostIP, isIcmpEchoRequest(buf, n, hostIP) {
                var reply = [UInt8](repeating: 0, count: n)
                buildIcmpEchoReply(&reply, buf, n, arpHostMAC, hostIP)
                netGuestQueue.send(reply)
                print("NET-ICMP: answered the guest's echo request for \(hostIP[0]).\(hostIP[1]).\(hostIP[2]).\(hostIP[3]) (id \(reply[38] << 8 | reply[39]), seq \(reply[40] << 8 | reply[41]))")
            }
            // M117b (#2474, ADR 0057 D10): the three v6 responders — the
            // same request-driven shape as the N3 ARP responder. A guest
            // Router Solicitation is answered with the hermetic RA (src
            // the router link-local, dst all-nodes — the guest's D2
            // filter admits it); an NS for a listed target (explicit
            // --net-ndp-respond entries + the router link-local when
            // --net-ra-respond is set) is answered with the NA; an
            // ICMPv6 echo request for the listed address is answered
            // byte-exact. Unlisted targets and unset flags go
            // unanswered — the refusal leg.
            if let pfx = netRaRespondPrefix, isRouterSolicitation(buf, n) {
                var reply = [UInt8](repeating: 0, count: 102)
                buildRouterAdvertisement(&reply, arpHostMAC, pfx)
                netGuestQueue.send(reply)
                print("NET-RA: answered the guest's router solicitation with an RA advertising \(v6Text(pfx))/64 from fe80::ff:fe00:2")
            }
            if isNeighborSolicitation(buf, n), let target = ndpRespondTarget(buf, n) {
                var reply = [UInt8](repeating: 0, count: 86)
                buildNeighborAdvertisement(&reply, buf, n, arpHostMAC, target)
                netGuestQueue.send(reply)
                print("NET-NDP: answered the guest's neighbor solicitation for \(v6Text(target)) (NA S=1 O=1, host MAC 02:00:00:00:00:02)")
            }
            if let addr = netIcmp6RespondAddr, isIcmp6EchoRequest(buf, n, addr) {
                var reply = [UInt8](repeating: 0, count: n)
                buildIcmp6EchoReply(&reply, buf, n, arpHostMAC)
                netGuestQueue.send(reply)
                print("NET-ICMP6: answered the guest's echo request for \(v6Text(addr)) (id \(reply[58] << 8 | reply[59]), seq \(reply[60] << 8 | reply[61]))")
            }
            // M117c (#2475, ADR 0057): the v6 transport responders — the
            // same request-driven shapes as N5/N10 on the fixed 40-byte
            // header (UDP at frame 54, TCP at frame 54). A listed
            // [addr]:port answers; anything else goes unanswered.
            if let addr = netUdp6RespondAddr, let port = netUdp6RespondPort,
               isUdp6Datagram(buf, n, addr, port) {
                var reply = [UInt8](repeating: 0, count: n)
                buildUdp6Reply(&reply, buf, n, arpHostMAC, addr, port)
                netGuestQueue.send(reply)
                let srcPort = (UInt16(buf[54]) << 8) | UInt16(buf[55])
                print("NET-UDP6: answered the guest's datagram for [\(v6Text(addr))]:\(port) (reply to guest src port \(srcPort), \(n - 62) payload bytes)")
            }
            if let addr = netTcp6RespondAddr, let port = netTcp6RespondPort,
               isTcp6Segment(buf, n, addr, port) {
                let flags = tcp6Flags(buf)
                let seq = tcp6Seq(buf)
                var reply = [UInt8](repeating: 0, count: 4096)
                var payload = [UInt8]()
                let dataOff = 54 + max(5, Int(buf[66] >> 4)) * 4
                if n > dataOff { payload = [UInt8](buf[dataOff..<n]) }
                let isSyn = (flags & 0x02) != 0 && (flags & 0x10) == 0
                let isFin = (flags & 0x01) != 0
                let isRst = (flags & 0x04) != 0
                let connKey = netTcp6ConnKey([UInt8](buf[22..<38]), (UInt16(buf[54]) << 8) | UInt16(buf[55]))
                if isRst {
                    netTcp6SrvConn(for: buf).closed = true
                    print("NET-TCP6: observed the guest's RST (seq 0x\(hex32(seq)))")
                } else if isSyn {
                    if !netTcpSrvAdmit(connKey) {
                        let replyLen = buildTcp6Reply(&reply, buf, n, arpHostMAC, port, 0, seq &+ 1, 0x14, [])
                        netGuestQueue.send(Array(reply[0..<replyLen]))
                        print("NET-TCP6: refused the guest's SYN (seq 0x\(hex32(seq))); \(netTcpSrvConnLimit) connections already live")
                    } else {
                        let conn = NetTcpSrvConn()
                        netTcpSrvConns[connKey] = conn
                        let replyLen = buildTcp6Reply(&reply, buf, n, arpHostMAC, port, netTcpSrvIsn, seq &+ 1, 0x12, [])
                        netGuestQueue.send(Array(reply[0..<replyLen]))
                        print("NET-TCP6: answered the guest's SYN (seq 0x\(hex32(seq))) with a SYN-ACK (seq 0x\(hex32(netTcpSrvIsn)), ack 0x\(hex32(seq &+ 1)))")
                    }
                } else if isFin {
                    let conn = netTcp6SrvConn(for: buf)
                    let replyLen = buildTcp6Reply(&reply, buf, n, arpHostMAC, port, conn.srvNxt, seq &+ 1, 0x11, payload)
                    netGuestQueue.send(Array(reply[0..<replyLen]))
                    conn.closed = true
                    print("NET-TCP6: answered the guest's FIN (seq 0x\(hex32(seq))) with a FIN-ACK (seq 0x\(hex32(conn.srvNxt)), ack 0x\(hex32(seq &+ 1)))")
                } else if !payload.isEmpty {
                    let conn = netTcp6SrvConn(for: buf)
                    var responsePayload = payload
                    let isGet = payload.count >= 4 && payload[0] == 0x47 && payload[1] == 0x45 && payload[2] == 0x54 && payload[3] == 0x20
                    if port == 80 || port == 8080 || isGet {
                        let body = "Hello from VirelaiOS Host!\n"
                        let httpBody = "HTTP/1.0 200 OK\r\nContent-Length: \(body.utf8.count)\r\n\r\n\(body)"
                        responsePayload = Array(httpBody.utf8)
                    }
                    let replyLen = buildTcp6Reply(&reply, buf, n, arpHostMAC, port, conn.srvNxt, seq &+ UInt32(payload.count), 0x10, responsePayload)
                    netGuestQueue.send(Array(reply[0..<replyLen]))
                    if isGet || port == 80 {
                        print("NET-TCP6: answered the guest's HTTP request with 200 OK (\(responsePayload.count) bytes)")
                    } else {
                        print("NET-TCP6: echoed the guest's \(payload.count)-byte data (ack 0x\(hex32(seq &+ UInt32(payload.count))), \(payload.count) payload bytes)")
                    }
                    conn.srvNxt = conn.srvNxt &+ UInt32(responsePayload.count)
                } else {
                    _ = netTcp6SrvConn(for: buf)
                    print("NET-TCP6: observed the guest's ACK (ack 0x\(hex32(tcp6Ack(buf))))")
                }
            }
            // Card N5: if the guest sent a UDP datagram to our ip:port,
            // echo it back from the host (same socket direction; the
            // capture file above keeps only guest TX, byte-exact,
            // unchanged).
            if let hostIP = netUdpRespondHostIP, let hostPort = netUdpRespondHostPort,
               isUdpDatagram(buf, n, hostIP, hostPort) {
                var reply = [UInt8](repeating: 0, count: n)
                buildUdpReply(&reply, buf, n, arpHostMAC, hostIP, hostPort)
                netGuestQueue.send(reply)
                let srcPort = (UInt16(buf[34]) << 8) | UInt16(buf[35])
                print("NET-UDP: answered the guest's datagram for \(hostIP[0]).\(hostIP[1]).\(hostIP[2]).\(hostIP[3]):\(hostPort) (reply to guest src port \(srcPort), \(n - 42) payload bytes)")
            }
            // Card N2 (claim 7566): DNS responder; M99i (#2187) added the
            // answer-set/depth knobs and the hostile injects. The noise
            // datagrams go out BEFORE the real reply so the guest's
            // rejection path is what lets the good answer through.
            if let hostIP = netDnsRespondHostIP, isDnsQuery(buf, n, hostIP, netDnsRespondHostPort) {
                let qname = extractDnsQName(buf, n)
                let qtype = dnsQueryType(buf, n)
                let answers = dnsAnswersFor(qtype, qname)
                let srcPort = (UInt16(buf[34]) << 8) | UInt16(buf[35])
                if netDnsSpoof {
                    var forged = [UInt8](repeating: 0, count: 1518)
                    let flen = buildDnsReply(&forged, buf, n, arpHostMAC, hostIP, netDnsRespondHostPort,
                                             answers: answers, ttl: netDnsTTL, truncate: false,
                                             forgeSrcIP: [10, 0, 0, 9])
                    if flen > 0 {
                        netGuestQueue.send(Array(forged[0..<flen]))
                        print("NET-DNS: injected a reply forged from 10.0.0.9 for '\(qname)' (the guest must drop it)")
                    }
                }
                if netDnsWrongQuestion {
                    var wq = [UInt8](repeating: 0, count: 1518)
                    let wlen = buildDnsReply(&wq, buf, n, arpHostMAC, hostIP, netDnsRespondHostPort,
                                             answers: answers, ttl: netDnsTTL, truncate: false,
                                             corruptQuestion: true)
                    if wlen > 0 {
                        netGuestQueue.send(Array(wq[0..<wlen]))
                        print("NET-DNS: injected a corrupted-question reply for '\(qname)' (the guest must drop it)")
                    }
                }
                var reply = [UInt8](repeating: 0, count: 1518)
                let replyLen = buildDnsReply(&reply, buf, n, arpHostMAC, hostIP, netDnsRespondHostPort,
                                             answers: answers, ttl: netDnsTTL, truncate: netDnsTruncate)
                if replyLen > 0 {
                    netGuestQueue.send(Array(reply[0..<replyLen]))
                    if netDnsTruncate {
                        print("NET-DNS: answered the guest's DNS query for '\(qname)' qtype=\(qtype) with TC=1 ANCOUNT=0 (reply to guest src port \(srcPort); the guest must refuse truncation)")
                    } else {
                        print("NET-DNS: answered the guest's DNS query for '\(qname)' qtype=\(qtype) ancount=\(answers.count) ttl=\(netDnsTTL) (reply to guest src port \(srcPort))")
                    }
                }
            }
            // M83b (#1775): SNTP responder
            if let sntp = netSntpResponder, let answer = sntp.answer(buf, n) {
                netGuestQueue.send(answer.frame)
                let originate = answer.originate.map { String(format: "%02x", $0) }.joined()
                print("NET-SNTP: answered \(answer.guestIP.map(String.init).joined(separator: ".")):\(answer.guestPort) served_unix=\(answer.servedUnix) skew=\(sntp.skew) originate=\(originate)")
            }
            // Card N8 (claim 0351): if the guest ran the DHCP client,
            // answer the handshake from the host — a tiny deterministic
            // DHCP server: DISCOVER (type 1) -> OFFER, REQUEST (type 3)
            // -> ACK, both with the FIXED gate-assertable lease (the
            // guest's xid echoed byte-exact, yiaddr = the lease IP, mask
            // 255.255.255.0, gateway 10.0.0.1, server id = the lease IP,
            // lease 3600). The reply goes broadcast (the client's flag)
            // so the guest's N2 MAC filter admits it.
            if let leaseIP = netDhcpRespondLeaseIP, isDhcpDatagram(buf, n),
               let mtype = dhcpMessageType(buf, n), mtype == 1 || mtype == 3 {
                // Audit follow-up 3 (issue #119): the refusal knobs. The
                // frame's dst MAC tells the REQUEST shape: the RENEWING
                // REQUEST is UNICAST to the server (02:00:00:00:00:02),
                // the INITIAL + REBINDING REQUESTs are BROADCAST. The
                // ciaddr (frame byte 54 = DHCP message byte 12) tells
                // INITIAL (0.0.0.0) from REBINDING (the lease). Refused
                // REQUESTs go unanswered — the guest's renewal stalls,
                // and with the autonomous idle-loop poll it escalates to
                // REBINDING at T2 / releases at expiry (the issue's
                // evidence).
                let dstBroadcast = buf[0] == 0xff && buf[1] == 0xff && buf[2] == 0xff &&
                                   buf[3] == 0xff && buf[4] == 0xff && buf[5] == 0xff
                let ciaddrSet = n > 58 && (buf[54] != 0 || buf[55] != 0 || buf[56] != 0 || buf[57] != 0)
                let refused = mtype == 3 && ((netDhcpRespondNoRenew && !dstBroadcast) ||
                                             (netDhcpRespondNoRebind && dstBroadcast && ciaddrSet))
                if refused {
                    print("NET-DHCP: refused the guest's \(!dstBroadcast ? "unicast RENEWING" : "broadcast REBINDING") REQUEST (xid 0x\(String(format: "%08x", (UInt32(buf[46]) << 24) | (UInt32(buf[47]) << 16) | (UInt32(buf[48]) << 8) | UInt32(buf[49])))) — the client must advance on its own")
                } else {
                var reply = [UInt8](repeating: 0, count: 4096)
                let replyLen = buildDhcpReply(&reply, buf, n, arpHostMAC, leaseIP, netDhcpRespondLeaseSecs, mtype)
                netGuestQueue.send(Array(reply[0..<replyLen]))
                // The xid hex built manually (Swift's String(format:) vararg
                // bridge mismatches %x with UInt8/UInt32 — the same reason
                // the N5 responder prints ports as arithmetic).
                let hexT = Array("0123456789abcdef")
                let xidHex = [hexT[Int(buf[46] >> 4)], hexT[Int(buf[46] & 0xf)],
                              hexT[Int(buf[47] >> 4)], hexT[Int(buf[47] & 0xf)],
                              hexT[Int(buf[48] >> 4)], hexT[Int(buf[48] & 0xf)],
                              hexT[Int(buf[49] >> 4)], hexT[Int(buf[49] & 0xf)]]
                print("NET-DHCP: answered the guest's DHCP \(mtype == 1 ? "DISCOVER" : "REQUEST") (xid 0x\(String(xidHex))) with a \(mtype == 1 ? "OFFER" : "ACK") for \(leaseIP[0]).\(leaseIP[1]).\(leaseIP[2]).\(leaseIP[3]) (lease \(netDhcpRespondLeaseSecs)s)")
                }
            }
            // Card N10 (claim 7026): if the guest's bounded TCP client
            // connected to our ip:port, answer the handshake + echo from
            // the host — a tiny deterministic TCP server: SYN -> SYN-ACK
            // (the FIXED gate-assertable server ISN, ack = the guest's
            // ISN+1), the handshake ACK -> observed, a data segment ->
            // ACK + the payload ECHOED byte-exact, FIN -> FIN-ACK, the
            // final ACK -> observed. The reply is a fresh frame in the
            // same socket direction as the other responders.
            if let hostIP = netTcpRespondHostIP, let hostPort = netTcpRespondHostPort,
               isTcpSegment(buf, n, hostIP, hostPort) {
                let flags = tcpFlags(buf)
                let seq = tcpSeq(buf)
                let ack = tcpAck(buf)
                var reply = [UInt8](repeating: 0, count: 4096)
                var payload = [UInt8]()
                // M99h (#2186): the payload starts at the segment's data
                // offset, not byte 54 — a guest SYN carrying its MSS
                // option must not be echoed as "payload" in the SYN-ACK.
                let dataOff = 34 + max(5, Int(buf[46] >> 4)) * 4
                if n > dataOff { payload = [UInt8](buf[dataOff..<n]) }
                let isSyn = (flags & 0x02) != 0 && (flags & 0x10) == 0
                let isFin = (flags & 0x01) != 0
                let isRst = (flags & 0x04) != 0
                // M99f (#2184): the per-connection peer, keyed by the
                // guest's src IP:port — several guest connections are
                // answered concurrently; a SYN on a live key restarts
                // only that connection.
                let connKey = netTcpConnKey([UInt8](buf[26..<30]), (UInt16(buf[34]) << 8) | UInt16(buf[35]))
                if isRst {
                    // The guest aborted — the connection is dead; observe.
                    let conn = netTcpSrvConn(for: buf)
                    conn.closed = true
                    if netTcpRespondRelayMode {
                        conn.relay.shutdown(suppressFin: true)
                        print("NET-TCP-RELAY: observed the guest's RST (seq 0x\(hex32(seq))); upstream closed")
                    } else if netTcpRespondSSHMode {
                        print("NET-TCP-SSH: observed the guest's RST (seq 0x\(hex32(seq)))")
                    } else {
                        print("NET-TCP: observed the guest's RST (seq 0x\(hex32(seq)))")
                    }
                } else if isSyn {
                    if !netTcpSrvAdmit(connKey) {
                        // The bounded server table is full — the honest
                        // refusal, the same one the guest's table sends.
                        let replyLen = buildTcpReply(&reply, buf, n, arpHostMAC, hostPort, 0, seq &+ 1, 0x14, [])
                        netGuestQueue.send(Array(reply[0..<replyLen]))
                        print("NET-TCP: refused the guest's SYN (seq 0x\(hex32(seq))); \(netTcpSrvConnLimit) connections already live")
                    } else {
                    let conn = NetTcpSrvConn()
                    if let old = netTcpSrvConns[connKey] { old.relay.shutdown(suppressFin: true) }
                    netTcpSrvConns[connKey] = conn
                    if netTcpRespondRelayMode, netTcpRespondRelayPort != nil {
                        // M51 interop (#1209): a fresh connection dials the
                        // real upstream NOW — real sshd sends its banner
                        // before the guest's version line, so the first
                        // paced chunk is already buffered.
                        print("NET-TCP-RELAY: connection accepted (guest SYN seq 0x\(hex32(seq))); dialing upstream \(netTcpRespondRelayHost):\(netTcpRespondRelayPort!)")
                        conn.relay.hostMAC = arpHostMAC
                        conn.relay.hostPort = hostPort
                        conn.relay.serverIsn = netTcpSrvIsn
                        conn.relay.onFrame = { frame in
                            netGuestQueue.send(frame)
                        }
                        conn.relay.begin(synFrame: buf, n)
                        conn.relay.start(netTcpRespondRelayHost, netTcpRespondRelayPort!)
                    } else if netTcpRespondSSHMode {
                        // M51 SSH5 (#1172): a fresh connection gets a fresh
                        // server state machine and an empty paced TX stream.
                        let cfg = SSHServer.Config(
                            hostKeySeed: netTcpRespondSSHHostKey,
                            acceptedUserKey: netTcpRespondSSHUserKey,
                            marker: netTcpRespondSSHMarker,
                            exitStatus: netTcpRespondSSHExitStatus,
                            tamperFirstServerTag: netTcpRespondSSHTamperMAC
                        )
                        let server = SSHServer(config: cfg)
                        server.logSink = { line in print("SSH-SRV: \(line)") }
                        conn.sshServer = server
                        print("NET-TCP-SSH: connection accepted (guest SYN seq 0x\(hex32(seq))); awaiting the version exchange")
                    }
                    let synAckOpts: [UInt8] = netTcpRespondOptionsMode ? netTcpOptionsSynAck : []
                    let replyLen = buildTcpReply(&reply, buf, n, arpHostMAC, hostPort, netTcpSrvIsn, seq &+ 1, 0x12, payload, options: synAckOpts)
                    netGuestQueue.send(Array(reply[0..<replyLen]))
                    print("NET-TCP: answered the guest's SYN (seq 0x\(hex32(seq))) with a SYN-ACK (seq 0x\(hex32(netTcpSrvIsn)), ack 0x\(hex32(seq &+ 1))\(netTcpRespondOptionsMode ? ", options mss/sackp/ws/ts" : ""))")
                    }
                } else if isFin {
                    let conn = netTcpSrvConn(for: buf)
                    if netTcpRespondHandshakeOnly {
                        // Card N11 (claim 5357): the handshake-only mode
                        // goes SILENT on data/FIN — the deterministic
                        // black hole that forces the guest's bounded
                        // retransmission machinery to fire.
                        print("NET-TCP: handshake-only — ignoring the guest's FIN (black hole)")
                    } else {
                        let finOpts: [UInt8] = netTcpRespondOptionsMode ? netTcpOptionsData : []
                        // #2349: in relay mode the connection's send-next
                        // lives on the relay (it advances as chunks pump);
                        // the conn's own srvNxt never moved.
                        let finSeq = netTcpRespondRelayMode ? conn.relay.currentSeqAck().seq : conn.srvNxt
                        let replyLen = buildTcpReply(&reply, buf, n, arpHostMAC, hostPort, finSeq, seq &+ 1, 0x11, payload, options: finOpts)
                        netGuestQueue.send(Array(reply[0..<replyLen]))
                        conn.closed = true
                        if netTcpRespondRelayMode {
                            conn.relay.shutdown(suppressFin: true)
                            print("NET-TCP-RELAY: the guest sent FIN; FIN-ACK and upstream close")
                        } else if netTcpRespondSSHMode {
                            print("NET-TCP-SSH: the guest sent FIN; FIN-ACK and close (SSH session done)")
                        } else {
                            print("NET-TCP: answered the guest's FIN (seq 0x\(hex32(seq))) with a FIN-ACK (seq 0x\(hex32(finSeq)), ack 0x\(hex32(seq &+ 1)))")
                        }
                    }
                } else if !payload.isEmpty {
                    let conn = netTcpSrvConn(for: buf)
                    // A data segment: the SSH responder's byte stream, the
                    // paced packet stream, or the HTTP/echo response.
                    if netTcpRespondRelayMode {
                        // M51 interop (#1209) + #2349: dedup the segment's
                        // payload against the in-order bound (retransmits
                        // write nothing upstream a second time), then pump
                        // as much real-server data as the guest's window
                        // holds — not one chunk per ACK. A dead relay is an
                        // RST, not a silent sink. No blocking wait: the
                        // guest waits on a bounded read and the upstream
                        // reader pumps asynchronously.
                        if conn.relay.guestDataSegment(buf, n) == .dead {
                            conn.closed = true
                        }
                        if !conn.closed && !conn.relay.pump() {
                            // Not established upstream bytes yet (or the
                            // guest's window is full): ACK the guest's
                            // data bare — at the in-order bound, so a
                            // dropped/ahead segment is NOT covered.
                            let sa = conn.relay.currentSeqAck()
                            let replyLen = buildTcpReply(&reply, buf, n, arpHostMAC, hostPort, sa.seq, sa.ack, 0x10, [])
                            netGuestQueue.send(Array(reply[0..<replyLen]))
                        }
                    } else if netTcpRespondSSHMode {
                        // M51 SSH5 (#1172): feed the SSH server with the
                        // guest's bytes and pace the next ≤192-byte chunk of
                        // its output with this ACK; when nothing is pending
                        // the ACK goes out bare.
                        if let server = conn.sshServer {
                            conn.sshTx.append(contentsOf: server.feed(payload))
                        }
                        if !netTcpRespondSSHSendChunk(conn, &reply, buf, n, arpHostMAC, hostPort, seq, payload.count) {
                            let replyLen = buildTcpReply(&reply, buf, n, arpHostMAC, hostPort, conn.srvNxt, seq &+ UInt32(payload.count), 0x10, [])
                            netGuestQueue.send(Array(reply[0..<replyLen]))
                        }
                    } else if netTcpRespondPacketMode {
                        // M51 SSH1 (#1168): a trigger/keepalive — ACK it bare
                        // and pace the next chunk; never echo (the payload is
                        // the SSH packet stream, not an echo surface).
                        if !netTcpSendPacketChunk(conn, &reply, buf, n, arpHostMAC, hostPort, seq, payload.count) {
                            let replyLen = buildTcpReply(&reply, buf, n, arpHostMAC, hostPort, conn.srvNxt, seq &+ UInt32(payload.count), 0x10, [])
                            netGuestQueue.send(Array(reply[0..<replyLen]))
                        }
                    } else if netTcpRespondHandshakeOnly {
                        print("NET-TCP: handshake-only — ignoring the guest's \(payload.count)-byte data (black hole)")
                    } else {
                        var responsePayload = payload
                        let isGet = payload.count >= 4 && payload[0] == 0x47 && payload[1] == 0x45 && payload[2] == 0x54 && payload[3] == 0x20
                        if hostPort == 80 || hostPort == 8080 || isGet {
                            let body = "Hello from VirelaiOS Host!\n"
                            let httpBody = "HTTP/1.0 200 OK\r\nContent-Length: \(body.utf8.count)\r\n\r\n\(body)"
                            responsePayload = Array(httpBody.utf8)
                        }
                        let dataOpts: [UInt8] = netTcpRespondOptionsMode ? netTcpOptionsData : []
                        let replyLen = buildTcpReply(&reply, buf, n, arpHostMAC, hostPort, conn.srvNxt, seq &+ UInt32(payload.count), 0x10, responsePayload, options: dataOpts)
                        netGuestQueue.send(Array(reply[0..<replyLen]))
                        if isGet || hostPort == 80 {
                            print("NET-TCP: answered the guest's HTTP request with 200 OK (\(responsePayload.count) bytes)")
                        } else {
                            print("NET-TCP: echoed the guest's \(payload.count)-byte data (ack 0x\(hex32(seq &+ UInt32(payload.count))), \(payload.count) payload bytes)")
                        }
                        conn.srvNxt = conn.srvNxt &+ UInt32(responsePayload.count)
                    }
                } else {
                    let conn = netTcpSrvConn(for: buf)
                    // A pure ACK (the handshake / the echo / the final ACK).
                    // M51 SSH1 (#1168): in packet mode the handshake ACK (and
                    // every later ACK for a delivered chunk) paces out the
                    // next chunk; M51 SSH5 (#1172): the same pacing feeds the
                    // encrypted SSH byte stream. Otherwise it is observed.
                    if netTcpRespondRelayMode {
                        // #2349: the ack/window pair is adopted only when
                        // it does not retreat — a retransmitted segment's
                        // stale snapshot can no longer stall the pump.
                        if conn.relay.guestAckSegment(buf, n) {
                            conn.closed = true // stalled past the bound; RST sent
                        } else if !conn.relay.pump() {
                            print("NET-TCP-RELAY: observed the guest's ACK (ack 0x\(hex32(ack)))")
                        }
                    } else if netTcpRespondSSHMode {
                        if !netTcpRespondSSHSendChunk(conn, &reply, buf, n, arpHostMAC, hostPort, seq, 0) {
                            print("NET-TCP-SSH: observed the guest's ACK (ack 0x\(hex32(ack)))")
                        }
                    } else if !netTcpSendPacketChunk(conn, &reply, buf, n, arpHostMAC, hostPort, seq, 0) {
                        print("NET-TCP: observed the guest's ACK (ack 0x\(hex32(ack)))")
                    }
                }
            }
            nativeSocketProbe?.receive(buf, n)
            // M99f (#2184): every host-initiated client is an independent
            // state machine — the intruder receipt and the main dispatch
            // are keyed by each client's own 5-tuple.
            for client in netTcpClients {
                if client.intruderSent, !client.intruderAnswered,
                   isTcpSegmentFromGuest(buf, n, client.guestIP, client.guestPort, client.cliPort &+ 1) {
                    client.intruderAnswered = true
                    let flags = tcpFlags(buf)
                    let verdict = (flags & 0x04) != 0 ? "refused (RST)" : ((flags & 0x12) == 0x12 ? "ADMITTED (SYN-ACK)" : "answered")
                    print("NET-TCP-INTRUDER: the guest \(verdict) flags=0x\(String(flags, radix: 16))")
                }
                // SH7 (#1083, ADR 0020 Amendment B): the host-initiated
                // inbound TCP client. Match the guest server's segments
                // (src = the guest listener) and drive the handshake /
                // data / close for THIS client only.
                if isTcpSegmentFromGuest(buf, n, client.guestIP, client.guestPort, client.cliPort) {
                    client.drive(buf, n)
                }
            }
        }
        try? netCaptureFile.synchronize()
        netCaptureDone.signal()
    }
    netCaptureThread?.start()
    let netAttachment = VZFileHandleNetworkDeviceAttachment(fileHandle: netCaptureSocket!)
    let netConfig = VZVirtioNetworkDeviceConfiguration()
    netConfig.attachment = netAttachment
    guard let fixedMAC = VZMACAddress(string: netMacText(netEffectiveMAC())) else {
        fail("Could not parse the fixed net MAC address.")
    }
    netConfig.macAddress = fixedMAC
    config.networkDevices = [netConfig]
} else if netNatEnabled {
    // Milestone five card N7 (claim 4678): the NAT attachment — the host
    // serves as the guest's router and performs NAT for accesses to
    // outside networks. The device config carries the SAME fixed
    // locally-administered MAC as the file-handle path; what the guest's
    // VIRTIO_NET_F_MAC read actually observes under NAT is a claim-time
    // observation (the NAT gateway may honor or override it — pinned in
    // the hardware contract, never assumed). No capture file: the host
    // translates the frames, so there are no guest TX bytes to capture.
    let natAttachment = VZNATNetworkDeviceAttachment()
    let natConfig = VZVirtioNetworkDeviceConfiguration()
    natConfig.attachment = natAttachment
    guard let fixedMAC = VZMACAddress(string: netMacText(netEffectiveMAC())) else {
        fail("Could not parse the fixed net MAC address.")
    }
    natConfig.macAddress = fixedMAC
    config.networkDevices = [natConfig]
} else if let bridgedIface = netBridgedIface {
    // M117d (issue #2476, ADR 0057 D11): the bridged attachment — the
    // guest shares the host's physical interface and sits on the real
    // LAN with the SAME fixed locally-administered MAC the other
    // attachments pin (the iface and the com.apple.vm.networking
    // entitlement were both checked by name during flag validation).
    let bridgedAttachment = VZBridgedNetworkDeviceAttachment(interface: bridgedIface)
    let bridgedConfig = VZVirtioNetworkDeviceConfiguration()
    bridgedConfig.attachment = bridgedAttachment
    guard let fixedMAC = VZMACAddress(string: netMacText(netEffectiveMAC())) else {
        fail("Could not parse the fixed net MAC address.")
    }
    bridgedConfig.macAddress = fixedMAC
    config.networkDevices = [bridgedConfig]
} else if netVmnetEnabled {
    // M117d (issue #2476, ADR 0057 D11): the vmnet attachment — the
    // shared-mode logical network was created up front (flag validation
    // fails carrying the vmnet_return_t on a non-success, so reaching
    // here means the network exists).
#if canImport(vmnet)
    let vmnetAttachment = VZVmnetNetworkDeviceAttachment(network: netVmnetNetwork!)
    let vmnetNetConfig = VZVirtioNetworkDeviceConfiguration()
    vmnetNetConfig.attachment = vmnetAttachment
    guard let fixedMAC = VZMACAddress(string: netMacText(netEffectiveMAC())) else {
        fail("Could not parse the fixed net MAC address.")
    }
    vmnetNetConfig.macAddress = fixedMAC
    config.networkDevices = [vmnetNetConfig]
#endif
} else {
    config.networkDevices = []
}
// Milestone fifteen card A1 (claim 6140): the virtio-snd device, attached
// only under `--sound`. One output stream with a host audio sink — the
// device the guest discovers (DID observed, not assumed: the 0x1040+type
// scheme predicts 0x1059). Without the flag the config is exactly as
// before: soundDevices = [] — every existing gate stays byte-identical.
if soundMode {
    let sound = VZVirtioSoundDeviceConfiguration()
    let output = VZVirtioSoundDeviceOutputStreamConfiguration()
    output.sink = VZHostAudioOutputStreamSink()
    sound.streams = [output]
    config.audioDevices = [sound]
    print("SOUND: virtio-snd attached (1 output stream, host sink)")
} else {
    config.audioDevices = []
}
#if SPIKE
if customVirtioEnabled, #available(macOS 27.0, *) {
    // The runtime guard above already requires macOS 27+; the availability
    // check exists only because the manifest floor (.v26, for the CI
    // toolchain) is below the custom-virtio APIs' 27.0 introduction. The
    // whole spike is additionally gated behind the SPIKE define: the CI
    // toolchain's SDK (macOS 26) does not declare the custom-virtio types
    // at all, so the base `swift build` must not reference them.
    print(CustomVirtioSpike.attach(to: config))
}
#else
if customVirtioEnabled {
    // Loud, never silent (issue #523 acceptance): this binary was compiled
    // WITHOUT -DSPIKE, so it has no custom-virtio code at all. Ignoring
    // the flag would boot a different machine than the caller asked for.
    fail("--custom-virtio/--cvc-echo/--via-virtio/--cvc-snap/--cvc-console-file/--snapshot-after require a SPIKE build (macOS 27 SDK types); rebuild with `swift build --package-path host/vm-runner --configuration release -Xswiftc -DSPIKE` (`zig build spike-virtio` drives it). This binary cannot attach the requested device.")
}
#endif
#if SPIKE
if gpu3dOptions.metal, #available(macOS 27.0, *) {
    switch GPU3DRunnerAttachment.assemble(options: gpu3dOptions) {
    case .failure(let why):
        fail("\(why)")
    case .success(let attachment):
        GPU3DRunnerRetain.attachment = attachment
        attachment.attach(to: config)
        gpu3dCountersReport = { attachment.counters() }
        print("  \(attachment.banner())")
    }
}
#endif
do { try config.validate() } catch { fail("Invalid VM configuration: \(error)") }
if vzRestore {
#if arch(arm64)
    // M70g G3 (#1459): name the attached extras so a verdict line is
    // self-describing when it lands in docs/hardware-contract.md. #2261:
    // each entry carries whether it is a VZ custom-virtio device — those
    // are the observed-unsaveable ones, and the refusal names them.
    var vzRestoreExtras: [(name: String, customVirtio: Bool)] = []
    if displayMode { vzRestoreExtras.append(("virtio-gpu", customVirtio: false)) }
    if inputMode { vzRestoreExtras.append(("usb-hid(xhci)", customVirtio: false)) }
    if usbMsdPath != nil { vzRestoreExtras.append(("usb-msd(xhci)", customVirtio: false)) }
    if customVirtioEnabled { vzRestoreExtras.append(("custom-virtio", customVirtio: true)) }
    if gpu3dOptions.metal { vzRestoreExtras.append(("gpu3d-metal", customVirtio: true)) }
    if virtioFSShareDir != nil { vzRestoreExtras.append(("virtio-fs", customVirtio: false)) }
    if vsockEnabled { vzRestoreExtras.append(("vsock", customVirtio: false)) }
    let vzRestoreDevices = vzRestoreExtras.isEmpty ? "headless-base" : vzRestoreExtras.map(\.name).joined(separator: ",")
    print("VZ-RESTORE: devices=\(vzRestoreDevices) mode=\(vzRestoreLoadDir != nil ? "load" : vzRestoreSaveDir != nil ? "save" : "same-process")")
    if vzWarmLeg {
        print("VZ-RESTORE: warm leg — restore → D5 handshake → scripted run; a restore refusal exits \(vzRestoreRefusedStatus) with one stdout line (ADR 0059 D8)")
    }
    do {
        try config.validateSaveRestoreSupport()
        print("VZ-RESTORE: validateSaveRestoreSupport passed; \(ProcessInfo.processInfo.operatingSystemVersionString), arm64")
    } catch {
        print("VZ-RESTORE: validateSaveRestoreSupport REFUSED devices=\(vzRestoreDevices) error=\(error as NSError)")
        // VZ cannot serialize custom-virtio device state and offers no
        // detach/quiesce API, so a configuration carrying one is refused
        // wholesale: name the blocking attachment(s), not just the list.
        let blocking = vzRestoreExtras.filter(\.customVirtio).map(\.name)
        fail("--vz-restore validateSaveRestoreSupport failed: \(error as NSError). No snapshot or reboot fallback. Blocking device(s): \(blocking.isEmpty ? vzRestoreDevices : blocking.joined(separator: ",")).")
    }
#else
    fail("--vz-restore requires arm64 save/restore APIs.")
#endif
}


runner.vm.delegate = vmDelegate
if hostSessionMode {
    // M139a (#2779): the menu bar's verbs route through the session
    // router — one process-global bar acts on the key window's bus once
    // a second window exists.
    hostApplication = HostApplication(serialLog: serialLogPath, inspector: inspector,
                                      menuTarget: VMSessionRegistry.shared.router)
    hostApplication?.onStop = { reason, code in stopHostSession(reason, code: code) }
}
primarySession.hostApp = hostApplication
primarySession.window = hostApplication?.window
// M128c (#2607): a document-launched session carries no `--profile` in
// argv — the models' argv resolution would leave them unarmed. Arm them
// on the clicked bundle so ⌘, and the Machine-menu verbs act on the
// machine that is actually running.
if let documentProfileURL {
    commandBus.settings.profileURL = documentProfileURL
    commandBus.profiles.profileURL = documentProfileURL
    commandBus.machine.profileURL = documentProfileURL
    // M120e (#2562): captures route into the clicked bundle's share/ too.
    commandBus.captures.profileURL = documentProfileURL
}
do { try config.validateSaveRestoreSupport() } catch {
    machineSaveUnsupported = "\(error as NSError)"
}
commandBus.machine.saveCapable = { machineSaveUnsupported == nil }
commandBus.machine.vmRunning = { [vm = runner.vm] in vm.state == .running }
commandBus.machine.suspendDriver = {
    let verdict = machineSuspendCtl.begin()
    guard verdict.accepted else { return verdict.code }
    machineSuspendCtl.finish()
    return nil
}
commandBus.machine.snapshotDriver = { name in
    let verdict = machineSnapshotCtl.save(name: name)
    machineSnapshotCtl.resumeAfterReply()
    return verdict.accepted ? nil : verdict.code
}
// The verbs the runner performs itself: the framebuffer capture the
// --screen/--screenshot-after path already owns (enabled once the display
// view exists), and a headless stop/quit for runs with no session window
// (in --host-session those verbs come from HostApplication's own
// registration instead).
if screenshotPath != nil || displayMode {
    var hostUIShotCount = 0
    commandBus.register(.screenshot, HostCommandBus.Registration(
        isEnabled: { machineView != nil },
        receiptDetail: { commandBus.captures.receiptDetail }
    ) {
        hostUIShotCount += 1
        let path = screenshotPathForCapture("hostui-\(hostUIShotCount)")
        writeScreenshot(to: path)
        guard FileManager.default.fileExists(atPath: path) else { return .failed }
        // M120e (#2562): a landed capture arms the saved badge + counter —
        // the receipt's file= is the name the badge shows.
        commandBus.captures.noteSaved(URL(fileURLWithPath: path))
        return .performed
    })
}
// M114b (#2455): the headless viewport verbs — the same two the session
// window registers on its own bus instance in HostApplication, here bound
// to the scripted run's evidence window + machine view so --host-cmd-after
// exercises the identical seam. (In --host-session HostApplication's own
// registrations stand; the runner never overwrites them.)
if !hostSessionMode, screenshotPath != nil || displayMode {
    commandBus.register(.fullscreen, HostCommandBus.Registration(
        isEnabled: { machineWindow?.isVisible ?? false },
        itemState: { machineWindow?.styleMask.contains(.fullScreen) ?? false }
    ) {
        machineWindow?.toggleFullScreen(nil)
        return .performed
    })
    commandBus.register(.captureKeys, HostCommandBus.Registration(
        isEnabled: { machineView != nil },
        itemState: { machineView?.capturesSystemKeys ?? false },
        receiptDetail: {
            guard let view = machineView else { return nil }
            return "capture=\(view.capturesSystemKeys ? "on" : "off")"
        }
    ) {
        guard let view = machineView else { return .failed }
        view.capturesSystemKeys.toggle()
        return .performed
    })
}
if !hostSessionMode {
    let headlessStop = HostCommandBus.Registration(isEnabled: { runner.vm.state == .running }) {
        runner.queue.async { runner.vm.stop { _ in } }
        return .performed
    }
    commandBus.register(.stop, headlessStop)
    commandBus.register(.quit, headlessStop)
}
// M114d (#2457): the inspector drawer verbs. The model's serialSink is
// the console pipe's write end wherever a guest RX path exists (the
// --script/--console duplex attachment, --vz-restore's probe input, or
// --host-session's read end) — without one the verb receipts `disabled`
// instead of writing into a pipe nobody reads. `send-serial` is the
// same registration in every mode, like screenshot: the drawer's Send
// and --host-cmd-after dispatch it identically.
if consoleMode || scriptMode || hostSessionMode || vzRestore {
    inspector.serialSink = { line in
        do {
            try consoleInputPipe.fileHandleForWriting.write(contentsOf: Data((line + "\n").utf8))
            return true
        } catch {
            return false
        }
    }
}
commandBus.register(.sendSerial, HostCommandBus.Registration(
    isEnabled: { inspector.serialSink != nil },
    receiptDetail: { "sent=\(inspector.sentCount)" }
) {
    inspector.inject() ? .performed : .failed
})
if !hostSessionMode {
    // Headless: no split pane exists, but the toggle is still honest —
    // isOpen is model state a receipt reports (the session window
    // registers its own pane-sliding variant in HostApplication).
    commandBus.register(.inspector, HostCommandBus.Registration(
        itemState: { inspector.isOpen },
        receiptDetail: { inspector.receiptDetail }
    ) {
        inspector.isOpen.toggle()
        return .performed
    })
}

print("VIRELAIOS VM runner")
print("  host: arm64 (Apple silicon), macOS \(osVersion.majorVersion).\(osVersion.minorVersion)")
print("  disk: \(overlayBasePath ?? diskImagePath)")
print("  memory: \(memoryMiB) MiB, cpus: \(cpuCount)")
if balloonEnabled {
    print("  balloon: ENABLED (M183b #3172: VZVirtioTraditionalMemoryBalloonDeviceConfiguration — host reclaim via targetVirtualMachineMemorySize; the guest's virtio_balloon driver inflates/deflates toward num_pages)")
}
for t in balloonTargetTriggers {
    print("  balloon-target-after: \"\(t.marker)\" → \(t.targetMiB) MiB (M183b: targetVirtualMachineMemorySize write on serial marker)")
}
if let p = activeProfile {
    print("  profile: \(p.url.path) identity=\(p.identity) (\(p.created ? "created" : "loaded"))")
}
if consoleMode {
    print("  mode: interactive console")
    print("  serial log: \(serialLogPath)  (guest output teed to terminal + log)")
    print("  interactive input: enabled — stdin → serial attachment (fileHandleForReading non-nil)")
    if let port = consoleTCPPort {
        let auth = consoleTCPSecret != nil
            ? ", HMAC-SHA256 challenge-response required (secret from \(consoleTCPSecretFromFile != nil ? "file" : "argv"))"
            : ""
        print("  console-tcp: \(consoleTCPBind):\(port) — remote console bridge (plaintext; #1066 Stage 0\(auth); one client at a time, extra clients told busy)")
    }
    print("  NOTE: guest RX is the polled virtio receive queue (claim 6684) — host bytes reach the kernel via the serial attachment")
    print("  controls: Ctrl-C ends the session and restores the terminal; Backspace/Enter are forwarded raw (no host line editing)")
} else if scriptMode {
    print("  mode: scripted input (non-interactive; claim 6684)")
    print("  serial log: \(serialLogPath)  (guest output teed to log)")
    print("  script: \(scriptPath!)  (forwarded once after the configured serial marker appears)")
    if let scriptAfter { print("  script-after: \"\(scriptAfter)\"  (forward once after this serial text appears)") }
    if let script2Path { print("  script2: \(script2Path)  (claim 4613: forwarded once after script2-after appears)") }
    if let script2After { print("  script2-after: \"\(script2After)\"  (forward script2 once after this serial text appears)") }
    if let script3Path { print("  script3: \(script3Path)  (claim 7786: forwarded once after script3-after appears)") }
    if let script3After { print("  script3-after: \"\(script3After)\"  (forward script3 once after this serial text appears)") }
    if let screenshotAfter { print("  screenshot-after: \"\(screenshotAfter)\"  (capture the framebuffer once after this serial text appears)") }
    if hostWriteAfter != nil {
        print("  host-write: \(hostWritePath ?? "<none>")  (M81f #1766: write \(hostWriteContent?.count ?? 0) bytes into the share once \"\(hostWriteAfter!)\" appears)")
    }
    if !snapshotTriggers.isEmpty {
        for t in snapshotTriggers { print("  snapshot-after: \"\(t.marker)\"  (claim 0680: kind-4 request over queue 3; guest streams the scanout → \(t.outPath))") }
        print("  snapshot-drain-timeout: \(snapshotDrainTimeout)s  (issue 1788: hold the VM at teardown until every armed kind-4 stream has landed; 0 = stop immediately)")
    }
    if let cvcConsoleFilePath { print("  cvc-console-file: \(cvcConsoleFilePath)  (claim 0680: structured console — every queue-1 log line captured; kind-3 arms the guest tee on \"cvconsole-ready\")") }
    if let scriptExpect { print("  script-expect: \"\(scriptExpect)\"  (exit 0 iff observed in the serial log)") }
    if scriptExpect != nil {
        print("  script-expect-tail: \(scriptExpectTail)s  (claim 4912: hold the VM after the match so post-marker kernel output reaches the serial log; 0 = stop immediately)")
    }
} else {
    print("  serial log: \(serialLogPath)  (timeout: \(Int(timeout))s)")
    print("  expecting: \"\(expectLine)\"")
    if let terminalMarker { print("  terminal marker: \"\(terminalMarker)\"") }
    if let markerDumpPath {
        print("  marker dump: \(markerDumpPath)  (ADR 0004 D4 fallback — NVRAM ladder; exit 0 iff an M2_* marker is found)")
    }
    if let nvramConsolePath {
        print("  nvram console: \(nvramConsolePath)  (claim 0015 — post-exit console stream from the NVRAM channel; exit 0 iff bytes were found)")
    }
}
for t in hostCmdTriggers {
    print("  host-cmd-after: \"\(t.marker)\" → \(t.command.rawValue)  (M114a #2454: HostCommand bus mirror — dispatch emits a host-ui: receipt)")
}
if let line = hostSerialSend {
    print("  host-serial-send: \(line.utf8.count) bytes armed  (M114d #2457: the send-serial verb / drawer injector writes the line into the console pipe)")
}
if hostCaptureKeys {
    print("  host-capture-keys: ENABLED (M114b #2455) — capture-keys dispatches on the HostCommand bus once the machine view exists; the host-ui: receipt is the same one ⌥⌘K / the toolbar pill emit")
}
if let netCapturePath {
    print("  net: ENABLED (milestone five card N1, claim 1373) — virtio-net device attached, guest TX frames captured byte-exactly to \(netCapturePath), MAC \(netMacText(netEffectiveMAC()))\(netMacOverride != nil ? " (--mac override)" : " (fixed)")")
}
if displayMode {
    print("  display: ENABLED (milestone six card G1, claim 6053) — virtio-gpu device attached, 1280x720 scanout window shown for the session")
}
if inputMode {
    print("  input: ENABLED (milestone seven card I1, claim 4272) — keyboard + pointing devices attached (VZUSBKeyboardConfiguration + VZUSBScreenCoordinatePointingDeviceConfiguration); the guest-side device is the Apple XHCI USB controller (DID 0x1a06) with the HID devices behind it")
}
if let usbMsdPath {
    print("  usb-msd: ENABLED (M43 card U6, issue #1037) — USB mass storage attached on an explicit VZXHCIControllerConfiguration (\(usbMsdPath)); the guest enumerates it as a bulk device behind the XHCI controller (the U1 bulk probe consumes it)")
}
if let dataDiskPath {
    print("  data-disk: ENABLED (M100b, issue #2307, ADR 0050) — second virtio-blk function id 'virelai-vlfs', raw read-write via kernel block device, sync .full (\(dataDiskPath)); the guest's GET_ID probe names it")
}
if let kc = inputKeyCode {
    if viaVirtioEnabled {
        print("  input-key: ENABLED (milestone seven card I2, claim 4116) — keyDown keyCode \(kc) rides the custom-virtio INPUT queue (claim 9588; HID-shaped messages, no view) after \"\(inputKeyAfter ?? "usb: enumerated")\"")
    } else {
        print("  input-key: ENABLED (milestone seven card I2, claim 4116) — synthesized keyDown keyCode \(kc) dispatched to the view after \"\(inputKeyAfter ?? "usb: enumerated")\" (the minimal I2 report seam; the full scripted surface is I3)")
    }
}
if let s = inputString {
    if viaVirtioEnabled {
        print("  input-string: ENABLED (milestone seven card I3, claim 6050) — typing \(s.debugDescription) over the custom-virtio INPUT queue (claim 9588; HID-shaped messages, no view) after \"\(inputStringAfter ?? "virelai> ")\"")
    } else {
        print("  input-string: ENABLED (milestone seven card I3, claim 6050) — typing \(s.debugDescription) into the view after \"\(inputStringAfter ?? "virelai> ")\" (keyDown + keyUp per char, shift for uppercase)")
    }
}
if let s = inputChords {
    if viaVirtioEnabled && !chordsViaView {
        print("  input-chords: ENABLED (milestone eight card U2, claim 1809) — typing \(s.debugDescription) over the custom-virtio INPUT queue (claim 9588; HID-shaped messages, no view, 0.25 s/stroke fixed pacing) after \"\(inputChordsAfter ?? "userspace: el0=1")\"")
    } else if chordsViaView {
        print("  input-chords: ENABLED (milestone eight card U2, claim 1809) — typing \(s.debugDescription) into the VZVirtualMachineView (--chords-view forces the view path despite the armed custom-virtio INPUT queue; \(inputChordsDelay) s per keystroke) after \"\(inputChordsAfter ?? "userspace: el0=1")\"")
    } else {
        print("  input-chords: ENABLED (milestone eight card U2, claim 1809) — typing \(s.debugDescription) into the view after \"\(inputChordsAfter ?? "userspace: el0=1")\" (keyDown + keyUp per chord: printable chars, return/up/down/left/right/home/end/delete/tab, alt-tab, ctrl-tab, ctrl-shift-tab, alt-shift-tab, ctrl-a..ctrl-z, ctrl-shift-a..ctrl-shift-z; \(inputChordsDelay) s per keystroke)")
    }
}
if let s = inputChords2 {
    if viaVirtioEnabled && !chordsViaView {
        print("  input-chords2: ENABLED — typing \(s.debugDescription) over the custom-virtio INPUT queue after \"\(inputChords2After ?? "userspace: el0=1")\"")
    } else {
        print("  input-chords2: ENABLED — typing \(s.debugDescription) into the view after \"\(inputChords2After ?? "userspace: el0=1")\"")
    }
}
if viaVirtioEnabled {
    print("  via-virtio: ENABLED (claim 9588, issue #523 item 3) — keyboard injection rides the custom-virtio INPUT queue as HID-shaped 16-byte messages; NO NSEvent/CGEvent synthesis and no window/view required (attacks #179 synthesized-drop and #151 activation wall)")
}
if let seq = pointerVirtioScript {
    print("  pointer-virtio: ENABLED (claim 9367, issue #523 item 3 / #151) — \(seq.debugDescription) rides the custom-virtio INPUT queue as kind-2 absolute-pointer messages after \"\(pointerVirtioAfter ?? "winloop: present ok")\" (transport=cv-input; headless-safe, no view)")
}
if let script = pointerScript {
    print("  pointer: ENABLED (milestone eight card U4, claim 4993) — \(script.debugDescription) after \"\(pointerAfter ?? "tasks user-el0 reaped")\" via route \"\(pointerRoute)\"; trust=post:\(CGPreflightPostEventAccess()) ax:\(AXIsProcessTrusted()) (request-trust=\(pointerRequestTrust ? "on" : "off"))")
}
if let netInjectPath {
    print("  net-inject: ENABLED (milestone five card N2, claim 6076) — \(netInjectPath) written into the attachment's socket once after \"\(netInjectAfter ?? "net: rx-armed")\" appears in the serial log (host→guest RX)")
}
if let hostIP = netArpRespondHostIP {
    let ipText = hostIP.map(String.init).joined(separator: ".")
    print("  net-arp-respond: ENABLED (milestone five card N3, claim 7293) — the host answers the guest's ARP requests for \(ipText) (host MAC 02:00:00:00:00:02) via the capture thread (deterministic, request-driven)")
}
if let hostIP = netIcmpRespondHostIP {
    let ipText = hostIP.map(String.init).joined(separator: ".")
    print("  net-icmp-respond: ENABLED (milestone five card N4, claim 0148) — the host answers the guest's ICMP echo requests for \(ipText) (host MAC 02:00:00:00:00:02) via the capture thread (deterministic, request-driven)")
}
if let pfx = netRaRespondPrefix {
    print("  net-ra-respond: ENABLED (M117b #2474, ADR 0057 D10) — the host answers the guest's router solicitations with an RA from fe80::ff:fe00:2 advertising \(v6Text(pfx))/64 (A+L, host MAC 02:00:00:00:00:02) via the capture thread (deterministic, request-driven)")
}
if !netNdpRespondAddrs.isEmpty || netRaRespondPrefix != nil {
    let names = netNdpRespondAddrs.map { v6Text($0) }.joined(separator: ", ")
    print("  net-ndp-respond: ENABLED (M117b #2474, ADR 0057 D10) — the host answers the guest's neighbor solicitations for [\(names)]\(netRaRespondPrefix != nil ? " plus the router link-local fe80::ff:fe00:2" : "") (NA S=1 O=1, host MAC 02:00:00:00:00:02) via the capture thread; unlisted targets go unanswered")
}
if let addr = netIcmp6RespondAddr {
    print("  net-icmp6-respond: ENABLED (M117b #2474, ADR 0057 D10) — the host answers the guest's ICMPv6 echo requests for \(v6Text(addr)) (host MAC 02:00:00:00:00:02) via the capture thread (deterministic, request-driven)")
}
if let addr = netUdp6RespondAddr, let port = netUdp6RespondPort {
    print("  net-udp6-respond: ENABLED (M117c #2475, ADR 0057) — the host echoes the guest's UDP/IPv6 datagrams for [\(v6Text(addr))]:\(port) (host MAC 02:00:00:00:00:02) via the capture thread (deterministic, request-driven)")
}
if let addr = netTcp6RespondAddr, let port = netTcp6RespondPort {
    print("  net-tcp6-respond: ENABLED (M117c #2475, ADR 0057) — the host answers the guest's bounded TCP/IPv6 client on [\(v6Text(addr))]:\(port) (host MAC 02:00:00:00:00:02, server ISN 0x\(hex32(netTcpSrvIsn))) via the capture thread (deterministic, request-driven)")
}
if let hostIP = netUdpRespondHostIP, let hostPort = netUdpRespondHostPort {
    let ipText = hostIP.map(String.init).joined(separator: ".")
    print("  net-udp-respond: ENABLED (milestone five card N5, claim 8552) — the host answers the guest's UDP datagrams for \(ipText):\(hostPort) (host MAC 02:00:00:00:00:02) via the capture thread (deterministic, request-driven)")
}
if let hostIP = netDnsRespondHostIP {
    let ipText = hostIP.map(String.init).joined(separator: ".")
    var knobs = "a=\(netDnsAnswers.count) aaaa=\(netDnsAnswers6.count) ttl=\(netDnsTTL)"
    if netDnsSpoof { knobs += " spoof" }
    if netDnsWrongQuestion { knobs += " wrong-question" }
    if netDnsTruncate { knobs += " truncate" }
    print("  net-dns-respond: ENABLED (milestone twelve card N2, claim 7566; depth M99i #2187) — the host answers the guest's DNS queries for \(ipText):\(netDnsRespondHostPort) via the capture thread (deterministic, request-driven; \(knobs))")
}
if let hostIP = netSntpRespondHostIP {
    let ipText = hostIP.map(String.init).joined(separator: ".")
    let skew = netSntpSkew ?? 0
    let skewText = skew == 0 ? "" : (skew > 0 ? "+\(skew)s" : "\(skew)s")
    print("  net-sntp-respond: ENABLED (M83b #1775) — the host answers the guest's SNTP queries for \(ipText):\(netSntpRespondHostPort) with host time\(skewText) via the capture thread (deterministic, request-driven)")
}
if let leaseIP = netDhcpRespondLeaseIP {
    let ipText = leaseIP.map(String.init).joined(separator: ".")
    // The prefix stays byte-identical to card N8's gate assertion; card
    // N9's lease knob is noted after it.
    print("  net-dhcp-respond: ENABLED (milestone five card N8, claim 0351) + card N9 (claim 9489) — the host answers the guest's DHCP handshake with the fixed lease ip=\(ipText) mask=255.255.255.0 gw=10.0.0.1 server=\(ipText) lease=\(netDhcpRespondLeaseSecs) via the capture thread (deterministic, request-driven)")
    if netDhcpRespondNoRenew {
        print("  net-dhcp-respond-norenew: ENABLED (audit follow-up 3, issue #119) — the host REFUSES the guest's unicast RENEWING REQUESTs; the client must escalate to REBINDING at T2 on its own")
    }
    if netDhcpRespondNoRebind {
        print("  net-dhcp-respond-norebind: ENABLED (audit follow-up 3, issue #119) — the host REFUSES the guest's broadcast REBINDING REQUESTs (ciaddr != 0); the client's lease runs out")
    }
}
if let hostIP = netTcpRespondHostIP, let hostPort = netTcpRespondHostPort {
    let ipText = hostIP.map(String.init).joined(separator: ".")
    print("  net-tcp-respond: ENABLED (milestone five card N10, claim 7026) + card N11 (claim 5357) — the host answers the guest's bounded TCP client on \(ipText):\(hostPort) (host MAC 02:00:00:00:00:02, server ISN 0x\(hex32(netTcpSrvIsn))) via the capture thread (deterministic, request-driven)")
    if netTcpRespondHandshakeOnly {
        print("  net-tcp-respond mode: handshake-only (card N11) — the SYN is answered with a SYN-ACK, then data/FIN go unanswered (the deterministic black hole for the retransmission-bound run)")
    }
    if netTcpRespondPacketMode {
        let chunkCount = (netTcpRespondPacketPayload.count + netTcpRespondPacketChunk - 1) / netTcpRespondPacketChunk
        print("  net-tcp-respond mode: packet (M51 SSH1, #1168) — after the handshake, page \(netTcpRespondPacketPayload.count) bytes out as \(chunkCount) chunks of ≤\(netTcpRespondPacketChunk) bytes, one per guest ACK")
    }
    if netTcpRespondOptionsMode {
        print("  net-tcp-respond mode: options (M99h, #2186) — the SYN-ACK carries MSS 1460 + SACK-permitted + WS + TS (data offset 10) and every data/FIN segment carries NOP+NOP+TS (data offset 8) — a real peer's wire shape")
    }
    if netTcpRespondSSHMode {
        print("NET-TCP-SSH: ENABLED (M51 SSH5, #1172) hostkey=\(SSHFixtures.hexString(netTcpRespondSSHHostKey)) userkey=\(SSHFixtures.hexString(netTcpRespondSSHUserKey)) marker=\(netTcpRespondSSHMarker.debugDescription) exit=\(netTcpRespondSSHExitStatus) tamper-mac=\(netTcpRespondSSHTamperMAC)")
    }
}
if let guestIP = netTcpConnectGuestIP, let guestPort = netTcpConnectPort {
    let ipText = guestIP.map(String.init).joined(separator: ".")
    print("  net-tcp-connect: ENABLED (M45 SH7, issue #1083, ADR 0020 Amendment B) — the host initiates an INBOUND TCP connection to the guest listener \(ipText):\(guestPort) (client port \(netTcpCliPort), ISN 0x\(hex32(netTcpCliIsn)), guest server ISN 0x\(hex32(netTcpGuestSrvIsn))) after \"\(netTcpConnectAfter ?? "sh: remote")\"\(netTcpConnectPayload.isEmpty ? "" : ", \(netTcpConnectPayload.count)-byte payload")")
    // M99f (#2184): each further --net-tcp-connect flag is its own client.
    for client in netTcpClients.dropFirst() {
        let extraIP = client.guestIP.map(String.init).joined(separator: ".")
        print("  net-tcp-connect: ENABLED additional client to \(extraIP):\(client.guestPort) (client port \(client.cliPort))")
    }
    if netTcpConnectSSHMode {
        let ptyDesc = netTcpConnectSSHPty.map { " pty=\($0.cols)x\($0.rows)" } ?? ""
        print("NET-TCP-CONNECT-SSH: ENABLED (M70g G1, #1491) user=\(netTcpConnectSSHUser) exec=\(netTcpConnectSSHExec.debugDescription) hostpin=\(SSHFixtures.hexString(netTcpConnectSSHHostPin))\(ptyDesc)")
        if let path = netTcpConnectSSHSessionFile {
            print("NET-TCP-CONNECT-SSH: session capture -> \(path) (M168f, #2962)")
        }
    }
    if let path = netTcpConnectStreamExec {
        print("NET-TCP-STREAM: ENABLED (hermetic --net only) executable=\(path)")
    }
}
if netNatEnabled {
    print("  net-nat: ENABLED (milestone five card N7, claim 4678) — VZNATNetworkDeviceAttachment attached (host router + NAT; no capture file — guest-observed counters are the gate's evidence)")
}
if let iface = netBridgedInterface {
    print("  net-bridged: ENABLED (M117d, issue #2476, ADR 0057 D11) — VZBridgedNetworkDeviceAttachment on \(iface) (com.apple.vm.networking carried in signature; the guest sits on the real LAN — guest-observed counters are the gate's evidence)")
}
if netVmnetEnabled {
    print("  net-vmnet: ENABLED (M117d, issue #2476, ADR 0057 D11) — VZVmnetNetworkDeviceAttachment on a shared-mode vmnet network\(netVmnetSubnetText.map { " (gateway \($0))" } ?? "") (the daemon serves DHCP + NAT; guest-observed counters are the gate's evidence)")
}

// M108 (#2325/#2326, ADR 0051): arm the live lanes BEFORE the VM starts —
// the ARMED record lands in the share now, so the seat's first poll after
// boot already verifies it. The session is SHARED: one credential, one
// epoch, one ARMED record naming every live lane in its mask. No flag →
// no HOSTINT/, no INBOX/, no records, no pasteboard read (D7's
// byte-silence: a flags-off boot is identical to today). A refused arm
// (no credential, broken share) is dead lanes, not a failed boot.
// The drop lane's display precondition (D4): the drop target is the
// display view, so a headless runner refuses lane_disabled at arm and the
// drop bit never enters the mask.
if hostClipboardFlag || hostDropFlag || hostNotifyFlag || hostOutboxFlag || hostSpeechFlag || hostPrintFlag {
    let shareURL = URL(fileURLWithPath: cvcFileShareDir!, isDirectory: true)
    var mask: UInt8 = 0
    var requested: [String] = []
    if hostClipboardFlag {
        mask |= HostInt.laneClip
        requested.append("clip")
    }
    var dropWanted = false
    if hostDropFlag {
        requested.append("drop")
        if screenshotPath != nil || displayMode || hostSessionMode {
            mask |= HostInt.laneDrop
            dropWanted = true
        } else {
            hostIntRefuse("drop", .laneDisabled)
        }
    }
    if hostNotifyFlag {
        mask |= HostInt.laneNotify
        requested.append("notify")
    }
    if hostOutboxFlag {
        mask |= HostInt.laneOutbox
        requested.append("outbox")
    }
    if hostSpeechFlag {
        mask |= HostInt.laneSpeech
        requested.append("speech")
    }
    if hostPrintFlag {
        mask |= HostInt.lanePrint
        requested.append("print")
    }
    if mask != 0 {
        hostIntSession = HostIntSession.arm(shareDir: shareURL, mask: mask, requested: requested)
    }
    if let session = hostIntSession {
        if hostClipboardFlag {
            hostClipboard = HostClipboard(session: session, pasteboardName: hostPasteboardName)
            hostClipboard?.start()
        }
        if dropWanted {
            hostDrop = HostDrop(session: session)
            hostDrop?.start()
        }
        if hostNotifyFlag {
            hostNotify = HostNotify(session: session)
            hostNotify?.start()
        }
        if hostOutboxFlag {
            // --host-outbox-dir redirects the landing; absent it the
            // lane default is ~/Downloads (the flag is what keeps a
            // class-B run out of the real one).
            let dest = hostOutboxDir.map { URL(fileURLWithPath: $0, isDirectory: true) }
                ?? FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first!
            hostOutbox = HostOutbox(session: session, destDir: dest)
            hostOutbox?.start()
        }
        if hostSpeechFlag {
            hostSpeech = HostSpeech(session: session)
            hostSpeech?.start()
        }
        if hostPrintFlag {
            // --host-print-dir redirects the degrade landing; absent it
            // the lane default is ~/Downloads (the flag is what keeps a
            // class-B run out of the real one).
            let dest = hostPrintDir.map { URL(fileURLWithPath: $0, isDirectory: true) }
                ?? FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first!
            hostPrint = HostPrint(session: session, destDir: dest)
            hostPrint?.start()
        }
        // One shared 250 ms drain for every consuming lane — the notify
        // and outbox lanes ride the same poll as the clipboard drain
        // (ADR 0051 D5, V2-D5).
        if hostClipboard != nil || hostNotify != nil || hostOutbox != nil || hostSpeech != nil || hostPrint != nil {
            DispatchQueue.main.async { hostIntTick() }
        }
    }
}

runner.queue.async {
    // M115a (#2460): arm the host vsock listener on the VM's queue BEFORE
    // any boot or restore — a post-restore guest connect must find the
    // listener already in place in the loading process too.
    if vsockEnabled {
        if let socketDevice = runner.vm.socketDevices.first as? VZVirtioSocketDevice {
            vsockListener.delegate = vsockListenerDelegate
            socketDevice.setSocketListener(vsockListener, forPort: vsockPort)
            print("VSOCK: host listener armed port=\(vsockPort)")
            // M115b (#2461, ADR 0056): the ctl endpoint is fail-closed —
            // no `vsock-ctl` share credential, no listener, and a guest
            // connect then gets the honest RST of an unlistened port.
            if vsockCtlEnabled {
                // M115d (#2463): the credential may ride EITHER host file
                // channel — the profile share can attach through cvc-file
                // or (for a save/restore-safe device set) virtio-fs.
                let ctlShareDirs = [cvcFileShareDir, virtioFSShareDir].compactMap { $0 }
                if let key = ctlShareDirs.lazy.compactMap({
                    VSCtl.loadKey(shareDir: URL(fileURLWithPath: $0, isDirectory: true))
                }).first {
                    // M115d (#2463): the suspend verb's driver — the
                    // profile's saved-state/ as destination, the
                    // device set's save/restore verdict as the support
                    // check. Both computed once here: the verb then
                    // answers `no_state`/`unsupported` honestly or runs
                    // the pause→save→reply→stop chain.
                    var suspendUnsupported: String? = nil
                    do { try config.validateSaveRestoreSupport() } catch {
                        suspendUnsupported = "\(error as NSError)"
                    }
                    let suspendCtl = SuspendCtl(vm: runner.vm, queue: runner.queue,
                                              savedStateDir: activeProfile?.savedStateURL,
                                              unsupported: suspendUnsupported)
                    let delegate = VSockCtlListenerDelegate(
                        key: key,
                        suspendHandler: { suspendCtl.begin() },
                        onSuspendAccepted: { suspendCtl.finish() })
                    vsockCtlDelegate = delegate
                    vsockCtlListener.delegate = delegate
                    socketDevice.setSocketListener(vsockCtlListener, forPort: vsockCtlPort)
                    print("VSOCK-CTL: host listener armed port=\(vsockCtlPort) suspend=\(suspendUnsupported != nil ? "unsupported" : (activeProfile == nil ? "no_state" : "armed"))")
                } else {
                    print("VSOCK-CTL: refuse no_credential (no vsock-ctl row in the armed share's SECRETS.TXT)")
                }
            }
        } else {
            FileHandle.standardError.write(Data("ERROR: --vsock was attached but no VZVirtioSocketDevice is present on the VM.\n".utf8))
            exit(1)
        }
    }
    if vzRestore, let load = vzRestoreLoadDir {
        // M70g G3 (#1459): the load process never boots — it restores the
        // other process's saved state into this fresh VZVirtualMachine.
        if vzWarmLeg {
            // ADR 0059 (M131b #2639): a scripted warm leg. Restore the leg's
            // checkpoint clone, then the D5 handshake (serial-epoch mark →
            // "\n" → fresh 'virelai> ' inside the epoch) seats the guest;
            // only then does the ordinary script/expect/timeout machinery
            // arm. The leg's first --script-after is re-based onto that
            // handshake — a boot-settle marker cannot re-fire post-restore.
            startGuestOutputTee()
            let probe = VZRestoreProbe(vm: runner.vm, queue: runner.queue, serialURL: serialURL,
                                       input: consoleInputPipe.fileHandleForWriting, timeout: timeout,
                                       mode: .load(URL(fileURLWithPath: load, isDirectory: true)),
                                       warm: true,
                                       onSeated: {
                vmDidStart = true
                print("VZ-RESTORE: warm leg seated — arming the leg's script over the post-restore epoch (ADR 0059 D6)")
                if let orig = scriptAfter {
                    print("  warm: --script-after \"\(orig)\" re-based onto the post-restore handshake (ADR 0059 D5)")
                }
                forwardScriptOnce(path: scriptPath!, after: "virelai> ", label: "script", settle: 0.5)
                startScript2Input()
                startScript3Input()
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { scriptPoll() }
            })
            probe.startFromSavedState()
            return
        }
        let probe = VZRestoreProbe(vm: runner.vm, queue: runner.queue, serialURL: serialURL,
                                   input: consoleInputPipe.fileHandleForWriting, timeout: timeout,
                                   witnessLine: vzRestoreWitness,
                                   verifyCommand: vzRestoreVerifyCommand,
                                   verifyOutput: vzRestoreVerifyOutput,
                                   mode: .load(URL(fileURLWithPath: load, isDirectory: true)))
        probe.startFromSavedState()
        return
    }
    if let resumeURL = resumeStateURL {
        // M115d (#2463): `--resume` — restore the machine state a ctl
        // `suspend` saved into the profile and continue the guest. The
        // ctl listener is already armed above, so the resumed machine
        // can suspend again.
        runner.vm.restoreMachineStateFrom(url: resumeURL) { error in
            if let error {
                print("SUSPEND-RESUME: restore refused code=\((error as NSError).code) error=\(error as NSError)")
                FileHandle.standardError.write(Data("ERROR: --resume restore failed: \(error as NSError)\n".utf8))
                exit(1)
            }
            runner.vm.resume { result in
                if case .failure(let error) = result {
                    print("SUSPEND-RESUME: resume refused error=\(error as NSError)")
                    FileHandle.standardError.write(Data("ERROR: --resume resume failed: \(error as NSError)\n".utf8))
                    exit(1)
                }
                vmDidStart = true
                print("SUSPEND-RESUME: restored \(SuspendCtl.stateFileName)")
            }
        }
        return
    }
    runner.vm.start { result in
        if case .failure(let error) = result {
            if hostSessionMode {
                FileHandle.standardError.write(Data("ERROR: VM failed to start: \(error)\n".utf8))
                DispatchQueue.main.async {
                    // M114d (#2457): a start refusal gets the crash card
                    // too — the drawer opens on it.
                    inspector.noteStop(PostmortemCard(
                        ending: .crashed, state: "start",
                        reason: StopReasonRecorder.describe(error),
                        serialLines: inspector.ingestedCount))
                    hostApplication?.showFailure("VM failed to start: \(error)")
                }
                return
            }
            restoreTerminal()
            FileHandle.standardError.write(Data("ERROR: VM failed to start: \(error)\n".utf8))
            exit(1)
        }
        vmDidStart = true
        if hostSessionMode {
            DispatchQueue.main.async { hostApplication?.didStart() }
        }
        if vzRestore {
            let mode: VZRestoreProbe.Mode = vzRestoreSaveDir.map { .save(URL(fileURLWithPath: $0, isDirectory: true)) } ?? .sameProcess
            let probe = VZRestoreProbe(vm: runner.vm, queue: runner.queue, serialURL: serialURL,
                                       input: consoleInputPipe.fileHandleForWriting, timeout: timeout,
                                       witnessLine: vzRestoreWitness,
                                       verifyCommand: vzRestoreVerifyCommand,
                                       verifyOutput: vzRestoreVerifyOutput,
                                       mode: mode, settleAfter: vzRestoreSettle)
            probe.start()
        }
    }
}








if hostSessionMode {
    installSignalHandlers()
    setupDisplayWindow()
    startGuestOutputTee()
    if scriptMode { startScriptInput() }
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { hostSessionPoll() }
} else if vzRestore {
    // A warm leg's timeout is owned by scriptPoll (same deadline), which
    // exits through the ordinary script verdict — the probe's hard fail
    // is only for the marker-query modes that otherwise never leave.
    if !vzWarmLeg {
        runner.queue.asyncAfter(deadline: .now() + timeout) {
            fail("--vz-restore timed out; no successful restore verdict.")
        }
    }
} else if consoleMode {
    // Install signal handlers BEFORE engaging raw/character mode so there is
    // never a window where the terminal is raw with no restore path.
    installSignalHandlers()
    setupTerminal()
    startConsoleStreams()
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { consolePoll() }
} else if scriptMode {
    // Claim 6684: non-interactive scripted input — tee guest output to the
    // log, forward the script after the terminal state, poll for the
    // expected transcript. Milestone six card G1 (claim 6053): when the
    // gpu device is attached (`--display`/`--screenshot`), the window is
    // created here too so the scripted run can capture the framebuffer.
    installSignalHandlers()
    setupDisplayWindow()
    startGuestOutputTee()
    startScriptInput()
    startScript2Input()
    startScript3Input()
    startNetInject()
    startNetTcpConnect()
    startNativeSocketProbe()
    startKeyInject()
    startKeyStringInject()
    startChordInject(csv: inputChords, after: inputChordsAfter)
    startChordInject(csv: inputChords2, after: inputChords2After)
    startPointerInject()
    startPointerVirtioInject()
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { scriptPoll() }
} else {
    installSignalHandlers()
    setupDisplayWindow()
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { poll() }
}
NativeInput.run(
    display: displayMode,
    input: inputMode,
    applicationLoop: {
        print("input: native AppKit event loop")
        NSApplication.shared.run()
    },
    headlessLoop: { RunLoop.main.run() }
)
