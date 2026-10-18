package lsm

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const sstMagic = 0x4E42534C // "LSBN"

type SSTable struct {
	path  string
	index []sstIndexEntry
	f     *os.File
}

type sstIndexEntry struct {
	key    []byte
	offset int64
}

func WriteSSTable(dir string, id int, pairs []kvPair) (*SSTable, error) {
	return writeSSTable(dir, id, pairs, publishSST)
}

func writeSSTable(dir string, id int, pairs []kvPair, publish func(string, string) error) (*SSTable, error) {
	path := filepath.Join(dir, fmt.Sprintf("sst-%06d.sst", id))
	f, err := os.CreateTemp(dir, ".sst-pending-*")
	if err != nil {
		return nil, err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()

	// Sort by key so the on-disk index supports binary search.
	sorted := make([]kvPair, len(pairs))
	copy(sorted, pairs)
	sort.Slice(sorted, func(i, j int) bool {
		return compareBytes(sorted[i].key, sorted[j].key) < 0
	})

	w := bufio.NewWriter(f)
	// Header
	var hdr [12]byte
	binary.BigEndian.PutUint32(hdr[0:4], sstMagic)
	binary.BigEndian.PutUint32(hdr[4:8], 1) // version
	binary.BigEndian.PutUint32(hdr[8:12], uint32(len(sorted)))
	w.Write(hdr[:])

	// Data blocks: each entry is [keyLen:4][valLen:4][key][value].
	// Offsets are computed arithmetically: f.Seek would report the underlying
	// file position, which lags behind the bufio.Writer buffer.
	index := make([]sstIndexEntry, len(sorted))
	off := int64(12)
	for i, p := range sorted {
		index[i] = sstIndexEntry{key: clone(p.key), offset: off}
		var lb [8]byte
		binary.BigEndian.PutUint32(lb[0:4], uint32(len(p.key)))
		binary.BigEndian.PutUint32(lb[4:8], uint32(len(p.value)))
		w.Write(lb[:])
		w.Write(p.key)
		w.Write(p.value)
		off += int64(8 + len(p.key) + len(p.value))
	}

	// Index block: for each entry [keyLen:4][key][offset:8]
	for _, e := range index {
		var lb [4]byte
		binary.BigEndian.PutUint32(lb[:], uint32(len(e.key)))
		w.Write(lb[:])
		w.Write(e.key)
		var ob [8]byte
		binary.BigEndian.PutUint64(ob[:], uint64(e.offset))
		w.Write(ob[:])
	}

	// Footer: index offset (== data end)
	var fb [8]byte
	binary.BigEndian.PutUint64(fb[:], uint64(off))
	w.Write(fb[:])

	if err := w.Flush(); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil { return nil, err }
	if err := f.Close(); err != nil {
		return nil, err
	}
	validated, err := OpenSSTable(f.Name())
	if err != nil { return nil, err }
	validated.Close()
	if err := publish(f.Name(), path); err != nil { return nil, err }

	// Reopen so the returned table carries a live file handle for reads.
	return OpenSSTable(path)
}

func OpenSSTable(path string) (*SSTable, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// Read footer (last 8 bytes)
	fi, _ := f.Stat()
	if fi.Size() < 20 {
		f.Close()
		return nil, fmt.Errorf("sstable too small")
	}
	var fb [8]byte
	f.ReadAt(fb[:], fi.Size()-8)
	indexOff := int64(binary.BigEndian.Uint64(fb[:]))

	// Read index block
	f.Seek(indexOff, 0)
	indexData := make([]byte, int(fi.Size()-8-indexOff))
	f.Read(indexData)
	index := parseIndex(indexData)

	f.Seek(0, 0)
	return &SSTable{path: path, index: index, f: f}, nil
}

func parseIndex(data []byte) []sstIndexEntry {
	var entries []sstIndexEntry
	pos := 0
	for pos < len(data) {
		if pos+4 > len(data) {
			break
		}
		keyLen := binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 4
		if pos+int(keyLen)+8 > len(data) {
			break
		}
		key := clone(data[pos : pos+int(keyLen)])
		pos += int(keyLen)
		offset := int64(binary.BigEndian.Uint64(data[pos : pos+8]))
		pos += 8
		entries = append(entries, sstIndexEntry{key: key, offset: offset})
	}
	return entries
}

func (s *SSTable) Get(key []byte) ([]byte, bool) {
	// Binary search index
	lo, hi := 0, len(s.index)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		cmp := compareBytes(s.index[mid].key, key)
		if cmp == 0 {
			return s.readEntry(s.index[mid].offset)
		} else if cmp < 0 {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return nil, false
}

func (s *SSTable) readEntry(offset int64) ([]byte, bool) {
	if s.f == nil {
		return nil, false
	}
	var lb [8]byte
	if _, err := s.f.ReadAt(lb[:], offset); err != nil {
		return nil, false
	}
	keyLen := binary.BigEndian.Uint32(lb[0:4])
	valLen := binary.BigEndian.Uint32(lb[4:8])
	buf := make([]byte, int(keyLen)+int(valLen))
	if _, err := s.f.ReadAt(buf, offset+8); err != nil {
		return nil, false
	}
	return buf[int(keyLen):], true
}

func (s *SSTable) Close() error {
	if s.f != nil {
		return s.f.Close()
	}
	return nil
}

func compareBytes(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func (s *SSTable) Size() int64 {
	fi, _ := os.Stat(s.path)
	if fi != nil {
		return fi.Size()
	}
	return 0
}
