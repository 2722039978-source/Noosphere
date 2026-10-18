package lsm

import "testing"

func TestMemTablePutGet(t *testing.T) {
	mt := NewMemTable()
	mt.Put([]byte("key1"), []byte("value1"))
	mt.Put([]byte("key2"), []byte("value2"))
	if v, ok := mt.Get([]byte("key1")); !ok || string(v) != "value1" {
		t.Fatal("key1 not found")
	}
	if v, ok := mt.Get([]byte("key2")); !ok || string(v) != "value2" {
		t.Fatal("key2 not found")
	}
	if _, ok := mt.Get([]byte("key3")); ok {
		t.Fatal("key3 should not exist")
	}
	if mt.Size() == 0 {
		t.Fatal("size should be > 0")
	}
}
func TestMemTableDelete(t *testing.T) {
	mt := NewMemTable()
	mt.Put([]byte("k"), []byte("v"))
	if !mt.Delete([]byte("k")) {
		t.Fatal("delete should succeed")
	}
	if _, ok := mt.Get([]byte("k")); ok {
		t.Fatal("key should be deleted")
	}
	if mt.Delete([]byte("x")) {
		t.Fatal("delete nonexistent should return false")
	}
}
func TestMemTableForEach(t *testing.T) {
	mt := NewMemTable()
	mt.Put([]byte("b"), []byte("2"))
	mt.Put([]byte("a"), []byte("1"))
	mt.Put([]byte("c"), []byte("3"))
	count := 0
	var last string
	mt.ForEach(func(k, v []byte) bool { count++; last = string(k); return true })
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
	if last != "c" {
		t.Fatalf("expected c as last, got %s", last)
	}
}
