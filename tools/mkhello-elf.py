#!/usr/bin/env python3
"""Emit a minimal statically linked AArch64 ELF32 executable (M22 D1, issue #324).

The program issues the claim-8215 EL0 syscalls directly:

    mov x8, #1          // sys_write
    mov x0, #1          // fd 1
    adr x1, msg
    mov x2, #len
    svc #0
    mov x0, #42         // exit status
    mov x8, #3          // sys_exit
    svc #0
    msg: "elf: hello from HELLO.ELF\\n"

Image layout (all little-endian): ELF header (52 B) at 0, one PT_LOAD
program header (32 B) at 52, code at 84, the message after the code. The
single segment is R+X at 0x00400000 — exactly the kernel loader's contract
(elf.zig `text_base`), so `exec HELLO.ELF` maps it read-only and enters at
e_entry. No libc, no sections, no relocations.

Usage: mkhello-elf.py [OUTPUT]   (default: zig-out/bin/HELLO.ELF)
"""

import struct
import sys

# Product version — the `ver M.m.p` the component-registry row shows
# and the drift check resolves (docs/component-registry.md); bump per
# the registry's rule. One line serves both rows this script produces:
# HELLO.ELF and CRASH.ELF (`--crash`).
VER = "1.0.0"

TEXT_BASE = 0x400000
EM_AARCH64 = 0xB7
PT_LOAD = 1
PF_X = 1
PF_R = 4


def movz(rd: int, imm16: int) -> int:
    """MOVZ (64-bit, hw=0): sf=1 opc=10 100101 hw imm16 Rd."""
    assert 0 <= rd <= 30
    assert 0 <= imm16 <= 0xFFFF
    return 0xD2800000 | (imm16 << 5) | rd


def adr(rd: int, rel: int) -> int:
    """ADR with a signed ±1 MiB pc-relative offset."""
    assert -(1 << 20) <= rel < (1 << 20)
    immlo = rel & 0x3
    immhi = (rel >> 2) & 0x7FFFF
    return 0x10000000 | (immlo << 29) | (immhi << 5) | rd


MSG = b"elf: hello from HELLO.ELF\n"
CODE_WORDS = 8
CODE_BYTES = CODE_WORDS * 4

# The ADR sits at instruction index 2 (pc = code + 8); the message follows
# the whole instruction block.
adr_rel = CODE_BYTES - 8

code = [
    movz(8, 1),        # x8 = 1 (sys_write)
    movz(0, 1),        # x0 = fd 1
    adr(1, adr_rel),   # x1 = &msg
    movz(2, len(MSG)), # x2 = len
    0xD4000001,        # svc #0
    movz(0, 42),       # x0 = exit status 42
    movz(8, 3),        # x8 = 3 (sys_exit)
    0xD4000001,        # svc #0
]

blob = b"".join(struct.pack("<I", w) for w in code) + MSG
assert len(blob) == CODE_BYTES + len(MSG)

ehsize, phentsize, phnum = 52, 32, 1
phoff = ehsize
code_off = ehsize + phnum * phentsize
filesz = len(blob)

# ELF header (ELF32): 4s magic + 5B ident tail + 3x pad + the u16/u32 fields.
ehdr = struct.pack(
    "<4s5B7xHHIIIIIHHHHHH",
    b"\x7fELF",
    1,  # EI_CLASS = ELF32
    1,  # EI_DATA = little-endian
    1,  # EI_VERSION
    0,  # EI_OSABI = SysV
    0,  # EI_ABIVERSION
    2,              # e_type = ET_EXEC
    EM_AARCH64,     # e_machine
    1,              # e_version
    TEXT_BASE,      # e_entry (== segment start)
    phoff,          # e_phoff
    0,              # e_shoff
    0,              # e_flags
    ehsize,         # e_ehsize
    phentsize,      # e_phentsize
    phnum,          # e_phnum
    0,              # e_shentsize
    0,              # e_shnum
    0,              # e_shstrndx
)
assert len(ehdr) == ehsize

# Program header: one PT_LOAD covering everything from code_off.
phdr = struct.pack(
    "<8I",
    PT_LOAD,
    code_off,                 # p_offset
    TEXT_BASE,                # p_vaddr
    TEXT_BASE,                # p_paddr
    filesz,                   # p_filesz
    filesz,                   # p_memsz
    PF_R | PF_X,              # p_flags — read-only text (W^X)
    0x1000,                   # p_align
)
assert len(phdr) == phentsize

image = ehdr + phdr + blob

# ---------------------------------------------------------------------------
# CRASH.ELF mode (--crash): a program that BRKs inside a named function so
# the tombstone can be symbol-resolved (M22 D3, issue #326). Same segment
# layout, plus section headers carrying a .symtab with one GLOBAL FUNC
# "crasher" covering the whole code block. BRK raises EC 0x3c from EL0 →
# status 139 with the PC at crasher+4 → tombstone note "(in crasher+0x4)".
# ---------------------------------------------------------------------------

def build_crash():
    code = [
        0xD503201F,     # nop                (+0)
        0xD4200000,     # brk #0             (+4 - the faulting PC)
        movz(8, 3),     # sys_exit           (never reached)
        movz(0, 99),    # status             (never reached)
        0xD4000001,     # svc #0             (never reached)
    ]
    blob = b"".join(struct.pack("<I", w) for w in code)

    strtab = b"\x00crasher\x00"
    shstrtab = b"\x00.symtab\x00.strtab\x00.shstrtab\x00"

    # Layout: [ehdr][phdr][code][strtab][symtab][shstrtab][shdrs]
    symtab_off = code_off + len(blob)
    symtab_len = 16 * 2                      # null + crasher
    strtab_off = symtab_off + symtab_len
    shstr_off = strtab_off + len(strtab)
    shoff = shstr_off + len(shstrtab)
    shnum = 4                                # NULL, .symtab, .strtab, .shstrtab

    # ELF32 Sym layout: st_name(4) st_value(4) st_size(4)
    #                  st_info(1) st_other(1) st_shndx(2)
    syms = struct.pack("<IIIBBH", 0, 0, 0, 0, 0, 0)          # reserved null
    syms += struct.pack("<IIIBBH",
                        1,                      # st_name -> "crasher"
                        TEXT_BASE,              # st_value
                        len(blob),              # st_size
                        (1 << 4) | 2,           # GLOBAL | FUNC
                        0,                      # st_other
                        1)                      # st_shndx (any defined sec)

    def shdr(name_idx, typ, off, size, link=0, entsize=0):
        return struct.pack("<10I", name_idx, typ, 0, 0, off, size,
                           link, 0, 4, entsize)

    sections = (
        struct.pack("<10I", *([0] * 10))                            # NULL
        + shdr(1, 2, symtab_off, len(syms), link=2, entsize=16)     # .symtab
        + shdr(9, 3, strtab_off, len(strtab))                       # .strtab
        + shdr(11, 3, shstr_off, len(shstrtab))                     # .shstrtab
    )

    ehdr_c = struct.pack(
        "<4s5B7xHHIIIIIHHHHHH",
        b"\x7fELF", 1, 1, 1, 0, 0,
        2,              # e_type = ET_EXEC
        EM_AARCH64, 1, TEXT_BASE, phoff, shoff, 0,
        ehsize, phentsize, 1, 40, shnum, 3,
    )
    phdr_c = struct.pack(
        "<8I", PT_LOAD, code_off, TEXT_BASE, TEXT_BASE,
        len(blob), len(blob), PF_R | PF_X, 0x1000,
    )
    # Concatenate in OFFSET order: [code][symtab][strtab][shstrtab][shdrs]
    image = ehdr_c + phdr_c + blob + syms + strtab + shstrtab + sections
    assert len(image) == shoff + shnum * 40
    return image


def main() -> None:
    argv = sys.argv[1:]
    crash = "--crash" in argv
    args = [a for a in argv if not a.startswith("--")]
    out = args[0] if args else "zig-out/bin/HELLO.ELF"
    if crash:
        img = build_crash()
        with open(out, "wb") as f:
            f.write(img)
        print(f"mkhello-elf: wrote {out} ({len(img)} bytes; "
              "BRK inside 'crasher' at +4 -> tombstone note '(in crasher+0x4)')")
        return
    with open(out, "wb") as f:
        f.write(image)
    print(f"mkhello-elf: wrote {out} ({len(image)} bytes; "
          f"entry=0x{TEXT_BASE:x} filesz={filesz})")


if __name__ == "__main__":
    main()
