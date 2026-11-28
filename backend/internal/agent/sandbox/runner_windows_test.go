package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLPACSeparatesWorkAndCredentials(t *testing.T) {
	parent := t.TempDir()
	work := filepath.Join(parent, "project")
	if e := os.Mkdir(work, 0700); e != nil {
		t.Fatal(e)
	}
	private := filepath.Join(parent, "private.key")
	if e := os.WriteFile(private, []byte("private-test-value"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("NOOSPHERE_BACKEND_TOKEN", "credential-fixture")
	result, e := Run(context.Background(), work, ".", "cmd", []string{"/d", "/c", "echo ok>result.txt & set NOOSPHERE_BACKEND_TOKEN & type ..\\private.key"})
	if e != nil {
		t.Fatal(e)
	}
	if result.ExitCode == 0 || strings.Contains(result.Output, "private-test-value") || strings.Contains(result.Output, "credential-fixture") {
		t.Fatalf("isolation: %+v", result)
	}
	raw, e := os.ReadFile(filepath.Join(work, "result.txt"))
	if e != nil || strings.TrimSpace(string(raw)) != "ok" {
		t.Fatalf("missing draft output %q, %v; %+v", raw, e, result)
	}
}
