package pdf

// Publication and runtime admission helpers share M89c's fixed error policy.
// They do not acquire, resize, serialize, diagnose or silently publish data.
func CheckRuntime(runtimeBytes uint64, regions uint32) Code {
	if runtimeBytes > 3_145_728 || regions > 11 {
		return MemoryLimit
	}
	return OK
}
func CheckOutput(bytes uint64) Code {
	if bytes > MaxOutput {
		return OutputLimit
	}
	return OK
}
func CheckReceipt(bytes uint64) Code {
	if bytes > MaxReceipt {
		return ReceiptLimit
	}
	return OK
}
func CheckDiagnostics(bytes uint64) Code {
	if bytes > MaxDiagnostics {
		return ReceiptLimit
	}
	return OK
}
func CheckEngineSize(fileDelta, initializedDelta uint64) Code {
	if fileDelta > 2_097_152 || initializedDelta > 2_097_152 {
		return EngineSizeLimit
	}
	return OK
}
func Operation() Failure { return fail(UnsupportedOperation) }
