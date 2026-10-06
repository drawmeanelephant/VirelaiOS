"""Two own-code, single-task fixtures using only write and sleep syscalls."""
import pathlib
import struct
import sys


def fixture(marker):
    message = (marker + "\n").encode("ascii")
    mov = lambda reg, value: 0xD2800000 | value << 5 | reg
    # write(fd=1, message, len); loop: sleep(1); branch loop.
    words = [mov(8, 1), mov(0, 1), 0x100000C1, mov(2, len(message)),
             0xD4000001, mov(8, 4), mov(0, 1), 0xD4000001, 0x17FFFFFD]
    # ADR x1 at byte 8 addresses the message at byte 36.
    words[2] = 0x10000000 | ((36 - 8) >> 2) << 5 | 1
    body = b"".join(struct.pack("<I", word) for word in words) + message
    header = struct.pack("<16sHHIQQQIHHHHHH", b"\x7fELF\x02\x01\x01" + bytes(9),
                         2, 183, 1, 0x400000, 64, 0, 0, 64, 56, 1, 0, 0, 0)
    segment = struct.pack("<IIQQQQQQ", 1, 5, 120, 0x400000, 0x400000,
                          len(body), len(body), 4096)
    return header + segment + body


if __name__ == "__main__":
    out = pathlib.Path(sys.argv[1])
    out.mkdir(parents=True, exist_ok=True)
    for name, marker in (("INITPRE.BIN", "initpre: ready"), ("INITDEP.BIN", "initdep: ready")):
        (out / name).write_bytes(fixture(marker))
