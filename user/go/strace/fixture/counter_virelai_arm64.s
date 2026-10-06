//go:build virelai && arm64

#include "textflag.h"

TEXT ·counterFrequency(SB),NOSPLIT,$0-8
	MRS CNTFRQ_EL0, R0
	MOVD R0, ret+0(FP)
	RET
