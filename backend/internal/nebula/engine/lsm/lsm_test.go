package lsm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Regression: a deleted key must stay deleted after its tombstone is flushed
// into an SSTable. The SSTable lookup path used to return the tombstone bytes
// as a live value, resurrecting the key.
func TestDeleteSurvivesFlush(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenLSM(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := l.Put([]byte("k"), []byte("v1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	l.mu.Lock()
	l.flush()
	l.mu.Unlock() // "k" now lives only in an SSTable

	if err := l.Delete([]byte("k")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	l.mu.Lock()
	l.flush()
	l.mu.Unlock() // tombstone now flushed into a newer SSTable

	if v, ok := l.Get([]byte("k")); ok {
		t.Fatalf("deleted key resurrected after flush: %q", v)
	}
	l.ForEach(func(k, v []byte) bool {
		if string(k) == "k" {
			t.Error("ForEach yielded deleted key")
		}
		return true
	})

	// And it must survive a reopen (SSTable tombstone read back from disk).
	l.Close()
	l2, err := OpenLSM(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer l2.Close()
	if v, ok := l2.Get([]byte("k")); ok {
		t.Fatalf("deleted key resurrected after reopen: %q", v)
	}
}

// Stress: concurrent writers, readers, deleters, stat/ForEach scanners and a
// flush-forcer all hammering one LSM. Run under -race to catch memory races;
// afterwards every key must hold one of the values written to it (or be absent).
func TestConcurrentMixedWorkload(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenLSM(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	const workers = 8
	const ops = 150

	var mu sync.Mutex
	written := make(map[string]map[string]bool) // key -> set of values ever written

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := fmt.Sprintf("key-%d-%d", w, i%10)
				val := fmt.Sprintf("val-%d-%d", w, i)
				l.Put([]byte(key), []byte(val))
				mu.Lock()
				if written[key] == nil {
					written[key] = make(map[string]bool)
				}
				written[key][val] = true
				mu.Unlock()

				if i%7 == 0 {
					l.Delete([]byte(fmt.Sprintf("key-%d-%d", w, (i+3)%10)))
				}
				if v, ok := l.Get([]byte(key)); ok {
					mu.Lock()
					valid := written[key][string(v)]
					mu.Unlock()
					if !valid {
						t.Errorf("key %s: read back foreign value %q", key, v)
					}
				}
				if i%25 == 0 {
					l.Stats()
					l.ForEach(func(k, v []byte) bool { return true })
				}
			}
		}(w)
	}

	// Concurrent flush-forcer: swaps memtable -> SSTable while others read/write.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops/2; i++ {
			l.mu.Lock()
			l.flush()
			l.mu.Unlock()
			time.Sleep(200 * time.Microsecond)
		}
	}()

	wg.Wait()

	// Final sweep: every value must belong to the key's write set.
	l.ForEach(func(k, v []byte) bool {
		mu.Lock()
		valid := written[string(k)][string(v)]
		mu.Unlock()
		if !valid {
			t.Errorf("final state: key %s holds foreign value %q", k, v)
		}
		return true
	})
	l.Close()
}

// Regression: the WAL append and the memtable update must be atomic. When the
// WAL was appended outside the lock, concurrent writes to the same key could
// leave a WAL order that disagrees with the memtable's last-write-wins state —
// a crash-replay would then restore a stale value. The WAL's final entry for a
// key must match the memtable's value for that key.
func TestWALOrderMatchesMemtable(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenLSM(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	key := []byte("shared-key")
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				l.Put(key, []byte(fmt.Sprintf("w%d-i%d", w, i)))
			}
		}(w)
	}
	wg.Wait()

	l.mu.RLock()
	memVal, memOK := l.mem.Get(key)
	l.mu.RUnlock()
	if !memOK {
		t.Fatal("memtable missing key after concurrent puts")
	}

	// Replay the WAL file independently and compare last-write per key.
	f, err := os.Open(filepath.Join(dir, "nebula.wal"))
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	defer f.Close()
	last := make(map[string][]byte)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e walEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		switch e.Op {
		case "put":
			last[e.Key] = e.Value
		case "del":
			last[e.Key] = tombstone
		}
	}
	walVal, ok := last[string(key)]
	if !ok {
		t.Fatal("WAL missing key after concurrent puts")
	}
	if !bytes.Equal(walVal, memVal) {
		t.Errorf("WAL/memtable divergence: wal=%q mem=%q (crash-replay would restore stale value)", walVal, memVal)
	}
}
