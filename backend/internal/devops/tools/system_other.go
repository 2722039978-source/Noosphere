//go:build !windows && !linux && !darwin

package tools

func diagnosticEnvironment() []string                       { return nil }
func diagnosticCommand(string, []string) (string, []string) { return "", nil }
