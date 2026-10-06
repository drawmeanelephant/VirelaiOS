//go:build virelai && arm64

#include "textflag.h"

TEXT ·counterFrequency(SB),NOSPLIT,$0-8
	MRS CNTFRQ_EL0, R0
	MOVD R0, ret+0(FP)
	RET

// sys_file_open(path, 128, flags=0) captures the input, then refuses EINVAL
// before file-table allocation or host I/O. The frame preserves R9 over SVC.
TEXT ·captureCallTicks(SB),NOSPLIT,$0-32
	MOVD path+0(FP), R0
	MOVD length+8(FP), R1
	MOVD $0, R2
	MOVD $23, R8
	ISB $15
	MRS CNTPCT_EL0, R9
	SVC $0
	ISB $15
	MRS CNTPCT_EL0, R10
	SUB R9, R10, R11
	MOVD R11, ticks+16(FP)
	MOVD R0, result+24(FP)
	RET
