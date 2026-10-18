package lsm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const memTableMaxSize = 4 * 1024 * 1024

var tombstone = []byte{0x00}

type LSM struct {
	mu        sync.RWMutex
	dir       string
	mem       *MemTable
	wal       *os.File
	sstables  []*SSTable
	nextSSTID int
	stats     Stats
	stopCh    chan struct{}
	closed    bool
}

type Stats struct {
	KeyCount   int64
	DiskBytes  int64
	WalEntries int64
	SSTCount   int
	MemTableKB int64
}

func OpenLSM(dir string) (*LSM, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	l := &LSM{dir: dir, mem: NewMemTable(), stopCh: make(chan struct{})}
	l.loadSSTables()
	if err := l.openWAL(); err != nil {
		return nil, err
	}
	if err := l.replayWAL(); err != nil {
		l.wal.Close()
		for _, sst := range l.sstables { sst.Close() }
		return nil, err
	}
	go l.periodicFlush()
	log.Printf("[lsm] opened: %d sstables, %d keys in mem", len(l.sstables), l.memSize())
	return l, nil
}

// Put appends to the WAL and updates the memtable in one critical section.
// Writing the WAL outside the lock allowed two races: (1) concurrent writes to
// the same key could persist a different last-write in WAL order than in the
// memtable, so a crash-replay would restore a stale value; (2) periodicFlush
// could snapshot the memtable and truncate the WAL between the two steps,
// losing the record entirely on crash.
func (l *LSM) Put(key, value []byte) error {
	l.mu.Lock()
	if err := l.appendWALLocked("put", key, value); err != nil {
		l.mu.Unlock()
		return err
	}
	l.mem.Put(key, value)
	needFlush := l.mem.Size() >= memTableMaxSize
	l.mu.Unlock()
	if needFlush {
		l.mu.Lock()
		l.flush()
		l.mu.Unlock()
	}
	return nil
}

func (l *LSM) Get(key []byte) ([]byte, bool) {
	// Lock the whole lookup: flush() swaps l.mem under the write lock, so an
	// unlocked memtable read can race and miss freshly moved keys.
	l.mu.RLock()
	defer l.mu.RUnlock()
	if v, ok := l.mem.Get(key); ok {
		if len(v) == 1 && v[0] == 0x00 {
			return nil, false
		}
		return v, true
	}
	for i := len(l.sstables) - 1; i >= 0; i-- {
		if v, ok := l.sstables[i].Get(key); ok {
			// Newest SSTable wins; a tombstone here masks older live values.
			// Without this check a deleted key resurrected after flush.
			if len(v) == 1 && v[0] == 0x00 {
				return nil, false
			}
			return v, true
		}
	}
	return nil, false
}

func (l *LSM) Delete(key []byte) error {
	l.mu.Lock()
	if err := l.appendWALLocked("del", key, nil); err != nil {
		l.mu.Unlock()
		return err
	}
	l.mem.Put(key, tombstone)
	l.mu.Unlock()
	return nil
}

func (l *LSM) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Stats{
		SSTCount:   len(l.sstables),
		WalEntries: l.stats.WalEntries,
		MemTableKB: int64(l.mem.Size()) / 1024,
	}
	for _, sst := range l.sstables {
		s.DiskBytes += sst.Size()
	}
	s.KeyCount = int64(l.memSize())
	return s
}

// ForEach iterates all live data: memtable first, then SSTables newest-first.
// A key is yielded at most once, from its newest location; tombstones mask
// older versions. The engine uses this on Open to rebuild the BM25 index —
// yielding stale SSTable versions would resurrect deleted memories.
func (l *LSM) ForEach(fn func(key, value []byte) bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	seen := make(map[string]bool)
	l.mem.ForEach(func(k, v []byte) bool {
		seen[string(k)] = true // memtable entry (incl. tombstone) masks all SSTables
		if len(v) == 1 && v[0] == 0x00 {
			return true
		}
		return fn(k, v)
	})
	for i := len(l.sstables) - 1; i >= 0; i-- {
		sst := l.sstables[i]
		for _, e := range sst.index {
			if seen[string(e.key)] {
				continue
			}
			v, ok := sst.readEntry(e.offset)
			if !ok {
				continue
			}
			seen[string(e.key)] = true
			if len(v) == 1 && v[0] == 0x00 {
				continue // tombstone masks older versions
			}
			if !fn(e.key, v) {
				return
			}
		}
	}
}

func (l *LSM) Close() {
	// Stop the periodic flusher before flushing: without this the goroutine
	// (and its ticker) leaked on every OpenLSM/Close cycle.
	close(l.stopCh)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	l.flush()
	for _, sst := range l.sstables {
		sst.Close()
	}
	if l.wal != nil {
		l.wal.Close()
	}
}

func (l *LSM) flush() {
	if l.mem.Size() == 0 {
		return
	}
	pairs := l.mem.Snapshot()
	sst, err := WriteSSTable(l.dir, l.nextSSTID, pairs)
	if err != nil {
		log.Printf("[lsm] sstable write error: %v", err)
		return
	}
	l.mem = NewMemTable()
	l.nextSSTID++
	l.sstables = append(l.sstables, sst)
	if len(l.sstables) > 5 {
		go l.compact()
	}
	if err := l.truncateWAL(); err != nil { log.Printf("[lsm] WAL retirement error (SST retained): %v", err) }
}

func (l *LSM) compact() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || len(l.sstables) < 3 {
		return
	}
	log.Printf("[lsm] compacting %d sstables", len(l.sstables))
	allPairs := make(map[string][]byte)
	for _, sst := range l.sstables {
		for _, e := range sst.index {
			if v, ok := sst.readEntry(e.offset); ok {
				allPairs[string(e.key)] = v
			}
		}
	}
	keys := make([]string, 0, len(allPairs))
	for k := range allPairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]kvPair, 0, len(keys))
	for _, k := range keys {
		v := allPairs[k]
		pairs = append(pairs, kvPair{key: []byte(k), value: v})
	}
	oldTables := l.sstables
	sst, err := WriteSSTable(l.dir, l.nextSSTID, pairs)
	if err != nil {
		log.Printf("[lsm] compaction write error: %v", err)
		return
	}
	l.sstables = nil
	for _, old := range oldTables {
		old.Close()
		os.Remove(old.path)
	}
	l.nextSSTID++
	l.sstables = append(l.sstables, sst)
	log.Printf("[lsm] compaction done: %d keys", len(pairs))
}

func (l *LSM) loadSSTables() {
	files, _ := filepath.Glob(filepath.Join(l.dir, "sst-*.sst"))
	l.nextSSTID = 0
	for _, f := range files {
		base := filepath.Base(f)
		idStr := strings.TrimSuffix(strings.TrimPrefix(base, "sst-"), ".sst")
		id, _ := strconv.Atoi(idStr)
		if id >= l.nextSSTID {
			l.nextSSTID = id + 1
		}
		sst, err := OpenSSTable(f)
		if err != nil {
			log.Printf("[lsm] open sstable %s error: %v", f, err)
			continue
		}
		l.sstables = append(l.sstables, sst)
	}
}

func (l *LSM) openWAL() error {
	walPath := filepath.Join(l.dir, "nebula.wal")
	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	l.wal = f
	return nil
}

// appendWALLocked appends one WAL entry. Caller must hold l.mu: Put/Delete
// write the WAL and memtable atomically under the same critical section.
func (l *LSM) appendWALLocked(op string, key, value []byte) error {
	entry := walEntry{Op: op, Key: string(key)}
	if value != nil {
		entry.Value = value
	}
	return l.writeEntryLocked(entry)
}

func (l *LSM) writeEntryLocked(entry walEntry) error {
	if l.wal == nil {
		return fmt.Errorf("WAL unavailable")
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > 64*1024*1024 {
		return fmt.Errorf("transaction exceeds WAL limit")
	}
	if _, err = l.wal.Write(data); err != nil {
		return err
	}
	if err = l.wal.Sync(); err != nil {
		return err
	}
	l.stats.WalEntries++
	return nil
}

// Batch is one WAL record: crash replay observes the whole project snapshot or none.
func (l *LSM) Batch(puts map[string][]byte, deletes []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.writeEntryLocked(walEntry{Op: "batch", Puts: puts, Deletes: deletes}); err != nil {
		return err
	}
	for _, key := range deletes {
		l.mem.Put([]byte(key), tombstone)
	}
	for key, value := range puts {
		l.mem.Put([]byte(key), value)
	}
	if l.mem.Size() >= memTableMaxSize {
		l.flush()
	}
	return nil
}

func (l *LSM) replayWAL() error {
	f, err := os.Open(filepath.Join(l.dir, "nebula.wal"))
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	var offset int64
	partial := false
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 { return i+1, data[:i], nil }
		if atEOF && len(data) > 0 { partial = true; return len(data), nil, nil }
		return 0, nil, nil
	})
	for scanner.Scan() {
		var entry walEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return fmt.Errorf("corrupt WAL record at byte %d: %w", offset, err)
		}
		switch entry.Op {
		case "put":
			l.mem.Put([]byte(entry.Key), entry.Value)
		case "del":
			l.mem.Put([]byte(entry.Key), tombstone)
		case "batch":
			for _, key := range entry.Deletes {
				l.mem.Put([]byte(key), tombstone)
			}
			for key, value := range entry.Puts {
				l.mem.Put([]byte(key), value)
			}
		default:
			return fmt.Errorf("unknown WAL operation at byte %d", offset)
		}
		offset += int64(len(scanner.Bytes()) + 1)
	}
	if err := scanner.Err(); err != nil { return fmt.Errorf("read WAL: %w", err) }
	if partial {
		// Only complete newline-terminated records are acknowledged. Remove a
		// torn, uncommitted suffix before any later append can be acknowledged.
		f.Close()
		l.wal.Close()
		if err := os.Truncate(filepath.Join(l.dir, "nebula.wal"), offset); err != nil { return err }
		if err := l.openWAL(); err != nil { return err }
		return l.wal.Sync()
	}
	return nil
}

func (l *LSM) truncateWAL() error {
	if l.wal != nil {
		l.wal.Close()
	}
	walPath := filepath.Join(l.dir, "nebula.wal")
	if err := os.Truncate(walPath, 0); err != nil { return err }
	if err := l.openWAL(); err != nil { return err }
	if err := l.wal.Sync(); err != nil { return err }
	l.stats.WalEntries = 0
	return nil
}

func (l *LSM) memSize() int {
	count := 0
	l.mem.ForEach(func(k, v []byte) bool { count++; return true })
	return count
}

func (l *LSM) periodicFlush() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			if l.mem.Size() > 0 {
				l.flush()
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

type walEntry struct {
	Op      string            `json:"op"`
	Key     string            `json:"key"`
	Value   []byte            `json:"value,omitempty"`
	Puts    map[string][]byte `json:"puts,omitempty"`
	Deletes []string          `json:"deletes,omitempty"`
}
