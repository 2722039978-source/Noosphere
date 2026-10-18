package lsm

import (
	"fmt"
	"testing"
)

// Regression: data must survive memtable flush into SSTable, including
// correct values read back via binary search on the index.
func TestFlushThenGet(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenLSM(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	const n = 50
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("session-%03d:mem-%03d", i%5, i)
		val := fmt.Sprintf(`{"id":"mem-%03d","payload":"value-%03d"}`, i, i)
		if err := l.Put([]byte(key), []byte(val)); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	// Force flush: memtable -> SSTable
	l.mu.Lock()
	l.flush()
	l.mu.Unlock()

	if len(l.sstables) != 1 {
		t.Fatalf("expected 1 sstable after flush, got %d", len(l.sstables))
	}

	for i := 0; i < n; i++ {
		key := fmt.Sprintf("session-%03d:mem-%03d", i%5, i)
		want := fmt.Sprintf(`{"id":"mem-%03d","payload":"value-%03d"}`, i, i)
		v, ok := l.Get([]byte(key))
		if !ok {
			t.Errorf("key %s: not found after flush", key)
			continue
		}
		if string(v) != want {
			t.Errorf("key %s: got %s, want %s", key, v, want)
		}
	}
}

// Regression: ForEach must iterate SSTable data too (used by engine
// rebuildIndex on restart), not only the live memtable.
func TestForEachIncludesSSTables(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenLSM(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k-%02d", i)
		if err := l.Put([]byte(key), []byte(fmt.Sprintf("v-%02d", i))); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	l.mu.Lock()
	l.flush()
	l.mu.Unlock()

	count := 0
	l.ForEach(func(k, v []byte) bool {
		count++
		return true
	})
	if count != 10 {
		t.Errorf("ForEach after flush: got %d keys, want 10", count)
	}
}
