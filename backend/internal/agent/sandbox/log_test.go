package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommandLogPaginatesAtUTF8Boundaries(t *testing.T) {
	work := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	var capture limitedOutput
	capture.Write([]byte("中文日志 ok"))
	result, err := persistLog(work, &capture)
	if err != nil {
		t.Fatal(err)
	}
	var whole string
	for int64(len(whole)) < result.LogBytes {
		part, err := ReadLog(t.Context(), work, result.LogID, int64(len(whole)), 4)
		if err != nil || part == "" {
			t.Fatalf("page %q, %v", part, err)
		}
		whole += part
	}
	if whole != "中文日志 ok" {
		t.Fatal(whole)
	}
	if _, err := ReadLog(t.Context(), work, result.LogID, 1, 8); err == nil {
		t.Fatal("accepted a split UTF-8 offset")
	}
}
