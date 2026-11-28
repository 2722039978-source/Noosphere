package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/agent/host"
)

func TestReadFileLinePaginationAndUTF8Alignment(t *testing.T) {
	root, d := fixture(t)
	content := "第一行：你好\n第二行：世界\n第三行：Noosphere\n第四行：完成\n"
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := openFreshDraft(t, root)
	if err != nil {
		t.Fatal(err)
	}
	e := engine{d: d}

	// Line-based pagination
	raw, err := e.dispatch(t.Context(), call("r1", "read_file", `{"path":"notes.txt","start_line":2,"end_line":3}`))
	if err != nil {
		t.Fatal(err)
	}
	res := raw.(map[string]any)
	if res["text"] != "第二行：世界\n第三行：Noosphere" || res["total_lines"] != 5 || res["start_line"] != 2 || res["end_line"] != 3 || res["next_line"] != 4 || res["done"] != false {
		t.Fatalf("unexpected line read result: %+v", res)
	}
	if len(res["short_hash"].(string)) != 12 {
		t.Fatalf("missing short_hash: %+v", res)
	}

	// Mid-rune UTF-8 byte offset aligns cleanly instead of failing.
	raw2, err := e.dispatch(t.Context(), call("r2", "read_file", `{"path":"notes.txt","offset":1,"limit":12}`))
	if err != nil {
		t.Fatal(err)
	}
	res2 := raw2.(map[string]any)
	if res2["binary_or_invalid_offset"] == true || !strings.HasPrefix(res2["text"].(string), "第一行") {
		t.Fatalf("expected gentle UTF-8 alignment, got: %+v", res2)
	}
	if res2["aligned_offset"] != int64(0) {
		t.Fatalf("expected aligned_offset=0, got: %+v", res2)
	}

	// search_files reports 1-based line number.
	sr, err := e.dispatch(t.Context(), call("s1", "search_files", `{"query":"Noosphere"}`))
	if err != nil {
		t.Fatal(err)
	}
	matches := sr.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["line"] != 3 {
		t.Fatalf("expected match on line 3, got %+v", matches)
	}
}

func TestObservedAndShortHashEditingGuards(t *testing.T) {
	root, _ := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("alpha\nbeta\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := openFreshDraft(t, root)
	if err != nil {
		t.Fatal(err)
	}
	e := engine{d: d}

	// Blind edit without reading first is rejected.
	if _, err := e.dispatch(t.Context(), call("e0", "edit_file", `{"path":"app.txt","edits":[{"old":"alpha","new":"ALPHA"}]}`)); err == nil {
		t.Fatal("expected blind edit to be rejected")
	}

	// Read observes revision; subsequent edit works without passing expected_hash.
	readOut, err := e.dispatch(t.Context(), call("r1", "read_file", `{"path":"app.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	short := readOut.(map[string]any)["short_hash"].(string)

	editOut, err := e.dispatch(t.Context(), call("e1", "edit_file", `{"path":"app.txt","edits":[{"old":"alpha","new":"ALPHA"}]}`))
	if err != nil {
		t.Fatalf("expected auto-hash edit to succeed: %v", err)
	}
	newShort := editOut.(map[string]any)["short_hash"].(string)
	if newShort == short || len(newShort) != 12 {
		t.Fatalf("expected updated short_hash, got %q vs %q", newShort, short)
	}

	// Old short hash prefix is rejected.
	if _, err := e.dispatch(t.Context(), call("e2", "edit_file", `{"path":"app.txt","expected_hash":"`+short+`","edits":[{"old":"beta","new":"BETA"}]}`)); err == nil {
		t.Fatal("expected stale short hash to be rejected")
	}

	// Current 12-char short hash prefix succeeds.
	if _, err := e.dispatch(t.Context(), call("e3", "edit_file", `{"path":"app.txt","expected_hash":"`+newShort+`","edits":[{"old":"beta","new":"BETA"}]}`)); err != nil {
		t.Fatalf("expected valid short hash to succeed: %v", err)
	}

	// Drift detection: mutate draft behind engine's back; omitting expected_hash must fail.
	_, currentHash, err := d.Stat(t.Context(), "app.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Replace(t.Context(), "app.txt", currentHash, []byte("drifted\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.dispatch(t.Context(), call("w1", "write_file", `{"path":"app.txt","content":"overwrite"}`)); err == nil {
		t.Fatal("expected drifted file write without re-read to be rejected")
	}
}

func openFreshDraft(t *testing.T, root string) (*host.Draft, error) {
	t.Helper()
	return host.Open(t.Context(), t.TempDir(), "test-run-ergo", root, "test-binding")
}
