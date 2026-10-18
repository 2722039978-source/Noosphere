package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/noosphere/backend/internal/nebula/engine/embed"
)

type rejectEmptyEmbedder struct{}
func (rejectEmptyEmbedder) Dim() int { return 2 }
func (rejectEmptyEmbedder) Embed(text string) ([]float32,error) { if text=="" {return nil,fmt.Errorf("empty input")};return []float32{1,0},nil }

func TestMetadataIsNotSentToEmbeddingProvider(t *testing.T) {
 e,err:=Open(DefaultOptions(t.TempDir()));if err!=nil{t.Fatal(err)};defer e.Close()
 e.PutText("__session_meta:empty",[]byte(`{"id":"empty"}`),"")
 e.PutText("session:content",[]byte("hello"),"hello")
 if err=e.SetEmbedder(rejectEmptyEmbedder{});err!=nil{t.Fatal(err)}
 if e.Stats().VectorCount!=1 {t.Fatal("metadata became a vector")}
}

func TestStructurallyCorruptVectorCacheIsRebuilt(t *testing.T) {
 dir:=t.TempDir();e,err:=Open(DefaultOptions(dir));if err!=nil{t.Fatal(err)}
 e.PutText("a",[]byte("a"),"alpha");e.PutText("b",[]byte("b"),"beta");e.Close()
 bad:=vectorSnapshot{Model:embeddingIdentity(embed.NewMockEmbedder(128)),Entries:map[string]cachedVector{"a":{Text:"alpha",Vector:[]float32{1}},"b":{Text:"beta",Vector:[]float32{1,0}}}}
 raw,_:=json.Marshal(bad);os.WriteFile(filepath.Join(dir,"vectors.json"),raw,0600)
 reopened,err:=Open(DefaultOptions(dir));if err!=nil{t.Fatalf("disposable cache blocked startup: %v",err)};defer reopened.Close()
 if reopened.Stats().VectorCount!=2{t.Fatal("cache not rebuilt")}
}

func TestDocumentCountAndMissingDelete(t *testing.T) {
	e, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for i := 0; i < 3; i++ {
		if err := e.PutText(fmt.Sprint(i), []byte("payload"), "alpha beta"); err != nil {
			t.Fatal(err)
		}
	}
	if e.totalDocs != 3 {
		t.Fatalf("document count=%d, want 3", e.totalDocs)
	}
	e.Delete("missing")
	if e.totalDocs != 3 {
		t.Fatal("missing delete changed document count")
	}
	e.Delete("1")
	if e.Stats().KeyCount != 2 || e.Stats().VectorCount != 2 {
		t.Fatalf("deleted record still indexed: %+v", e.Stats())
	}
}

func TestChineseRetrieval(t *testing.T) {
	e, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.PutText("project:a", []byte("一"), "数据库连接超时，请检查网络")
	e.PutText("project:b", []byte("二"), "用户头像上传成功")
	hits := e.bm25Search("连接超时", 3)
	if len(hits) != 1 || hits[0].Key != "project:a" {
		t.Fatalf("Chinese keyword results: %+v", hits)
	}
	vec, _ := e.Embed("数据库连接")
	nonzero := false
	for _, n := range vec {
		if n != 0 {
			nonzero = true
		}
	}
	if !nonzero {
		t.Fatal("Chinese mock embedding is zero")
	}
}

func TestRestartPreservesIndexTextAndRepairsVectorSnapshot(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	e.PutText("one", []byte(`{"secret_field":"hiddennoise"}`), "searchable content")
	e.Close()
	if err := os.WriteFile(filepath.Join(dir, "vectors.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	e, err = Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if hits := e.bm25Search("hiddennoise", 3); len(hits) != 0 {
		t.Fatalf("raw value polluted rebuilt index: %+v", hits)
	}
	if hits := e.bm25Search("searchable", 3); len(hits) != 1 {
		t.Fatalf("index text lost: %+v", hits)
	}
	if e.Stats().VectorCount != 1 {
		t.Fatal("vector snapshot was not recovered")
	}
	value, _ := e.Get("one")
	if string(value) != `{"secret_field":"hiddennoise"}` {
		t.Fatalf("stored value changed: %s", value)
	}
}

func TestEmbeddingDimensionChangeRebuildsExistingVectors(t *testing.T) {
	e, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.PutText("one", []byte("payload"), "database connection")
	e.SetEmbedder(embed.NewMockEmbedder(256))
	hits, err := e.Search("database connection", 3)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search after model change: %+v %v", hits, err)
	}
}

func TestLegacyMemoryIndexUsesContent(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(map[string]any{"content": "database retry", "summary": "connection", "embedding": []float32{123456}})
	if err := e.lsm.Put([]byte("old"), value); err != nil {
		t.Fatal(err)
	}
	e.Close()
	e, err = Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if len(e.bm25Search("123456", 1)) != 0 {
		t.Fatal("legacy embedding indexed as text")
	}
	if len(e.bm25Search("retry", 1)) != 1 {
		t.Fatal("legacy content lost")
	}
}
