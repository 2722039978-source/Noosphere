package evalrunner

// ValidateCompileSpec applies the same source allowlist as Compile without
// compiling or executing any source. Dataset imports use it at admission.
func ValidateCompileSpec(spec CompileSpec) error { return validateSource(spec) }
