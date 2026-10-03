/* VirelaiOS kernel linker script.
 *
 * The default lld layout aligns LOAD segments to the 64 KiB max page size,
 * which would pad the flat kernel image (KERNEL.BIN) with ~64 KiB of zeros.
 * This script lays the loadable sections out densely from address 0, so the
 * produced image is a few hundred bytes. _start (the kernel entry) is placed
 * first, making the entry point sit at image base + 0.
 *
 * The kernel is only ever loaded as a flat image (tools/elf2bin.py) at a
 * 4K-aligned address; all addressing is PC-relative, so the exact vaddrs
 * chosen here are irrelevant at runtime. See docs/decisions/
 * 0002-kernel-handoff.md.
 */
ENTRY(_start)
SECTIONS {
    . = 0x0;
    .text : ALIGN(16) { *(.text .text.*) }
    /* Claim 8215: page-isolated EL0 code. The MMU grants only this range
     * EL0 read/execute permission; padding prevents neighboring kernel text
     * from sharing a user-accessible page. */
    .usertext : ALIGN(4096) {
        __user_text_start = .;
        KEEP(*(.usertext))
        . = ALIGN(4096);
        __user_text_end = .;
    }
    .rodata : ALIGN(16) { *(.rodata .rodata.*) }
    .data.rel.ro : ALIGN(16) { *(.data.rel.ro .data.rel.ro.*) }
    .data : ALIGN(16) { *(.data .data.*) }
    /* Dedicated EL0 RW stack pages; kept separate from privileged BSS. */
    .userbss (NOLOAD) : ALIGN(4096) {
        __user_stack_start = .;
        KEEP(*(.userbss))
        . = ALIGN(4096);
        __user_stack_end = .;
    }
    .bss : ALIGN(16) { *(.bss .bss.*) }
    .ARM.exidx : ALIGN(16) { *(.ARM.exidx*) }
    /* The kernel has no DWARF unwinder or language exceptions. B7 removes
     * unused unwind metadata, not the 16 MiB KRN2 loader bound; current main
     * had only 1368 bytes left under that bound. Frame-pointer diagnostics
     * and host-test ELF unwind tables are unaffected. */
    /DISCARD/ : { *(.note.*) *(.eh_frame .eh_frame_hdr) }
}
