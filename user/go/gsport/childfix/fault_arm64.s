#include "textflag.h"

// faultBrk executes a BRK instruction. BRK is a debug-class EL0 exception
// (ESR_EL1.EC 0x3c), which the kernel deliberately does NOT deliver to a
// process's registered fault handler — it takes the reap path and records
// the exit as the reserved fault status 139. That makes BRK the
// deterministic unhandled-fault a Go program can emit: a data abort would
// instead route through sigpanic and recover as an exit-2 panic.
//
// The word is emitted literally because the portable assembler's BRK
// spelling has varied across toolchains: encoding is 0xd4200000 | imm<<5.
TEXT ·faultBrk(SB), NOSPLIT, $0-0
	WORD $0xd42000e0 // BRK #7
	RET
