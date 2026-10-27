package modellab

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/noosphere/backend/internal/codelens"
)

func TestCodeLensFixtureHashAndGoSymbols(t *testing.T) {
	d, err := LoadDataset(DatasetID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range d.Cases() {
		e, _ := d.Evidence(ref)
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, "fixture.go", e.Faulty, 0)
		if err != nil {
			t.Fatal(err)
		}
		fn := f.Decls[0].(*ast.FuncDecl)
		start, end := fs.Position(fn.Pos()), fs.Position(fn.End())
		path := filepath.Join(t.TempDir(), e.ID+".go")
		entity := codelens.Entity{Name: e.Function, Type: "function", Line: start.Line, EndLine: end.Line, StartByte: start.Offset, EndByte: end.Offset}
		a := codelens.Analysis{File: path, Language: "go", SizeBytes: len(e.Faulty), Lines: end.Line, Entities: []codelens.Entity{entity}}
		if _, err = validateAnalysis(e, path, []byte(e.Faulty), a); err != nil {
			t.Fatal(err)
		}
		a.Entities[0].EndByte--
		if _, err = validateAnalysis(e, path, []byte(e.Faulty), a); err == nil {
			t.Fatal("forged range accepted")
		}
	}
}

func TestContextRejectsUnavailableTamperedAndForeignFixtures(t *testing.T) {
	d, err := LoadDataset(DatasetID)
	if err != nil {
		t.Fatal(err)
	}
	ref := d.Cases()[0]
	e, _ := d.Evidence(ref)
	b, err := NewContextBuilder(codelens.NewAdapter([]string{"unused"}, "unused", 1), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err = b.BuildContext(context.Background(), d, ref); err == nil {
		t.Fatal("unavailable CodeLens must fail")
	}
	file := filepath.Join(b.root, e.ID+".go")
	if err = os.WriteFile(file, []byte("foreign source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = b.BuildContext(context.Background(), d, ref); err == nil {
		t.Fatal("tampered fixture accepted")
	}
	if err = confinedFixture(b.root, filepath.Join(filepath.Dir(b.root), "foreign.go"), false); err == nil {
		t.Fatal("escape accepted")
	}
	// Root ownership must hold before contacting an unavailable worker.
	if _, err = b.BuildContext(context.Background(), d, CaseRef{}); err == nil {
		t.Fatal("forged ref accepted")
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = b.BuildContext(context.Background(), d, ref); err == nil {
		t.Fatal("closed builder accepted")
	}
}

func TestEmbeddedHashAndTypedAssertionValidation(t *testing.T) {
	base := "benchmarks/" + DatasetID + "/"
	raw, err := benchmarkFiles.ReadFile(base + "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) ([]byte, error) { return benchmarkFiles.ReadFile(base + path) }
	for _, name := range []string{"manifest", "case", "fixture", "reference", "assertion-type"} {
		t.Run(name, func(t *testing.T) {
			var m datasetManifest
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			mutateRead := read
			switch name {
			case "manifest":
				m.PromptHash = "forged"
			case "case":
				m.Cases[0].Hash = "forged"
			case "fixture", "reference":
				mutateRead = func(path string) ([]byte, error) {
					if filepath.Base(path) == name+".go.txt" || name == "fixture" && filepath.Base(path) == "faulty.go.txt" {
						return []byte("forged"), nil
					}
					return read(path)
				}
			case "assertion-type":
				path := "cases/" + m.Cases[0].ID + "/case.json"
				body, _ := read(path)
				var e CaseEvidence
				_ = json.Unmarshal(body, &e)
				e.Assertions[0].Expected = []byte(`{"pass":true}`)
				body, _ = json.Marshal(e)
				m.Cases[0].Hash = digest(body)
				mutateRead = func(p string) ([]byte, error) {
					if p == path {
						return body, nil
					}
					return read(p)
				}
			}
			mutated, _ := json.Marshal(m)
			if _, err := loadDatasetBytes(mutated, mutateRead); err == nil {
				t.Fatalf("accepted corrupted %s", name)
			}
		})
	}
}
