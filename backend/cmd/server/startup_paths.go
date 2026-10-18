package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Resolve environments relative to installed resources, never the writable data
// directory or the shell's working directory. Explicit user settings take priority.
func resolveCodeLensPython(configured string, scripts []string) [][]string {
	if configured != "" && configured != "python" {
		return [][]string{{configured}}
	}
	var commands [][]string
	seen := make(map[string]bool)
	for _, script := range scripts {
		root := filepath.Dir(filepath.Dir(script))
		roots := []string{root, filepath.Dir(root)}
		if filepath.Base(root) == "python" {
			roots = append(roots, filepath.Dir(filepath.Dir(root)))
		}
		for _, root := range roots {
			for _, env := range []string{".venv", filepath.Join(".local", "venv")} {
				for _, binary := range []string{filepath.Join("Scripts", "python.exe"), filepath.Join("bin", "python")} {
					candidate := filepath.Clean(filepath.Join(root, env, binary))
					if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && !seen[candidate] {
						commands = append(commands, []string{candidate})
						seen[candidate] = true
					}
				}
			}
		}
	}
	return append(commands, []string{"python"}, []string{"python3"}, []string{"py", "-3"})
}

func resolveCodeLensScripts(explicit, workingDir, executable string) ([]string, error) {
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			return nil, fmt.Errorf("NOOSPHERE_CODELENS_SCRIPT must be an absolute path")
		}
		if info, err := os.Stat(explicit); err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("CodeLens script is missing: %s", explicit)
		}
		return []string{filepath.Clean(explicit)}, nil
	}
	candidates := []string{
		filepath.Join(workingDir, "codelens-python", "main.py"),
		filepath.Join(workingDir, "python", "codelens", "main.py"),
		filepath.Join(filepath.Dir(executable), "..", "codelens-python", "main.py"),
	}
	var scripts []string
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && !seen[candidate] {
			scripts = append(scripts, candidate)
			seen[candidate] = true
		}
	}
	if len(scripts) == 0 {
		return nil, fmt.Errorf("CodeLens resources are missing; reinstall the complete application")
	}
	return scripts, nil
}
