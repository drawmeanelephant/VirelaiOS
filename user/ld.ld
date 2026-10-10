ENTRY(_start)
SECTIONS {
    . = 0x00800000;
    .text : ALIGN(4096) {
        *(.text .text.*)
        *(.rodata .rodata.*)
    }
    . = ALIGN(4096);
    .data : ALIGN(4096) {
        *(.data .data.*)
        *(.bss .bss.*)
    }
    /DISCARD/ : { *(.note.*) *(.eh_frame .eh_frame_hdr) }
}
