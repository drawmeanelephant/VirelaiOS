# M91 input vectors

`native-descriptor.hex` is the 138-byte HID report descriptor from the
coach's `artifacts/live-xhci-serial-01.log`, macOS 27.2, 2026-10-03.
`native-motion.hex` contains the three distinct complete 10-byte packets,
in first-occurrence order, from
`artifacts/m87-desktop-acceptance.g9iR6a/session-serial.log`. The diagnostic
printed a packet length followed by each byte as a 64-bit hex word.
These are genuine VZ USB captures from the dirty coach checkout at
`fc21e531`, not captures from main or from this implementation.

`native-keyboard-descriptor.hex` is the 60-byte descriptor extracted from
this implementation's `live-xhci` gate on 2026-10-03 (base `03d929f2`,
uncommitted M91b changes). That same gate re-observed the pinned pointer
descriptor byte-exact. The keyboard descriptor has E0..E7 modifier bits,
one reserved byte, and a six-entry key array.

Local source copies are under `artifacts/m91-input/`:

| Source copy | SHA-256 |
|---|---|
| `coach-descriptor.log` | `f2114f1efc5d286c42f7567eb665e3d83093e5b530fa69539a865d17ad7e5089` |
| `coach-native-capture.log` | `81246adf9a2dd9018f4462bc9444424a4bc09415ac1b5af31451f099fd1cdf22` |
| `native-descriptor-current.log` | `95894b4e7f0c94c9c6133c0a3d861d9c9a7846e6aa9b5b8f41f84cf2648403ab` |

The descriptor identifies report ID 1, three buttons, and two absolute
16-bit axes with logical range 0..32767. The motion packets all have
buttons up. They do not establish click acceptance or physical input
acceptance of the corrected implementation.

`canonical-abi.hex` is **synthetic**, pinned from the documented five-byte
custom-virtio ABI. Its zero/max-axis and down/held/up cases, the native
button/endpoint derivatives, and the keyboard reports in unit tests are
constructed regressions, **not captured gestures**. Genuine native
button-down/up and raw keyboard capture vectors are a remaining capture
gap; tests and gate input injection do not replace human acceptance.
