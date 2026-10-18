package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCodeLensPythonFindsProjectEnvironmentFromReleaseResources(t *testing.T) {
	project := t.TempDir()
	python := filepath.Join(project, ".local", "venv", "Scripts", "python.exe")
	if err := os.MkdirAll(filepath.Dir(python), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{filepath.Join(project, "release", "codelens-python", "main.py"), filepath.Join(project, "backend", "python", "codelens", "main.py")} {
		commands := resolveCodeLensPython("python", []string{script})
		if commands[0][0] != python {
			t.Fatalf("environment not found independently of data directory: %v", commands)
		}
	}
	explicit := resolveCodeLensPython("custom-python.exe", []string{filepath.Join(project, "release", "codelens-python", "main.py")})
	if !reflect.DeepEqual(explicit, [][]string{{"custom-python.exe"}}) {
		t.Fatalf("explicit setting ignored: %v", explicit)
	}
	want := [][]string{{"python"}, {"python3"}, {"py", "-3"}}
	if got := resolveCodeLensPython("", nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("system fallbacks changed: %v", got)
	}
}

func TestCodeLensResourcesIndependentOfDataDirectory(t *testing.T) {
	resources := t.TempDir()
	script := filepath.Join(resources, "codelens-python", "main.py")
	if err := os.MkdirAll(filepath.Dir(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("# fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	for _, explicit := range []string{script, ""} {
		result, err := resolveCodeLensScripts(explicit, data, filepath.Join(resources, "bin", "backend.exe"))
		if err != nil || len(result) != 1 || result[0] != script {
			t.Fatalf("scripts=%v error=%v", result, err)
		}
	}
}

func TestExplicitCodeLensScriptDoesNotFallBackToUnrelatedWorkingDirectory(t *testing.T) {
	data := t.TempDir()
	for _, invalid := range []string{"relative.py", filepath.Join(data, "missing.py"), data} {
		if _, err := resolveCodeLensScripts(invalid, data, ""); err == nil {
			t.Fatalf("accepted invalid override: %s", invalid)
		}
	}
}
