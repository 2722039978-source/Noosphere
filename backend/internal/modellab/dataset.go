package modellab

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/noosphere/backend/internal/evalrunner"
	"github.com/noosphere/backend/internal/modelstore"
)

const DatasetID = "codetask-go-v1"
const ScorerVersion = "host-exact-v1.0.0"
const PromptVersion = "repair-go-v1.0.0"
const promptTemplate = "Repair the function below. Return only solution.go text: package main and exactly the specified function, no imports, markdown, patches, explanations or test claims. Keep the exact finite signature. Only the function body may change.\n"

//go:embed benchmarks/codetask-go-v1
var benchmarkFiles embed.FS

//go:embed scorer.go
var scorerSource []byte

type Assertion struct {
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
}
type CaseEvidence struct {
	ID            string      `json:"id"`
	Version       string      `json:"version"`
	Category      string      `json:"category"`
	Task          string      `json:"task"`
	Function      string      `json:"function"`
	InputType     string      `json:"input_type"`
	OutputType    string      `json:"output_type"`
	FixtureHash   string      `json:"fixture_sha256"`
	ReferenceHash string      `json:"reference_sha256"`
	Assertions    []Assertion `json:"assertions"`
	Faulty        string      `json:"-"`
	Reference     string      `json:"-"`
	CaseHash      string      `json:"-"`
}
type datasetManifest struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	Source        string `json:"source"`
	License       string `json:"license"`
	ScorerVersion string `json:"scorer_version"`
	ScorerHash    string `json:"scorer_sha256"`
	PromptVersion string `json:"prompt_version"`
	PromptHash    string `json:"prompt_sha256"`
	Cases         []struct {
		ID   string `json:"id"`
		Hash string `json:"sha256"`
	} `json:"cases"`
}

// CaseRef is an opaque capability binding an embedded dataset and case revision.
// Callers cannot override signatures, assertions, or expected results.
type CaseRef struct{ datasetHash, id, caseHash string }
type Dataset struct {
	revision   modelstore.DatasetRevision
	cases      []CaseEvidence
	scorerHash string
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func jsonCopy[T any](v T) T {
	b, _ := json.Marshal(v)
	var copy T
	_ = json.Unmarshal(b, &copy)
	return copy
}
func strictJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

var caseIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

func LoadDataset(id string) (*Dataset, error) {
	if id != DatasetID {
		return nil, fmt.Errorf("unknown built-in dataset %q", id)
	}
	base := "benchmarks/" + DatasetID + "/"
	raw, err := benchmarkFiles.ReadFile(base + "manifest.json")
	if err != nil {
		return nil, err
	}
	return loadDatasetBytes(raw, func(path string) ([]byte, error) { return benchmarkFiles.ReadFile(base + path) })
}
func loadDatasetBytes(raw []byte, read func(string) ([]byte, error)) (*Dataset, error) {
	var m datasetManifest
	if err := strictJSON(raw, &m); err != nil {
		return nil, err
	}
	if m.ID != DatasetID || m.Version != "1.0.0" || len(m.Cases) != 12 || m.Source == "" || m.License != "CC0-1.0" || m.ScorerVersion != ScorerVersion || m.ScorerHash != digest(scorerSource) || m.PromptVersion != PromptVersion || m.PromptHash != digest([]byte(promptTemplate)) {
		return nil, fmt.Errorf("dataset manifest/version/contract hash mismatch")
	}
	hash := digest(raw)
	d := &Dataset{scorerHash: m.ScorerHash, revision: modelstore.DatasetRevision{ID: m.ID + "." + m.Version, BenchmarkID: m.ID, Version: m.Version, ContentHash: hash, ScorerVersion: m.ScorerVersion, RunnerVersion: "evalrunner-t1-wasi-v1", PromptTemplateHash: m.PromptHash, License: m.License, Source: m.Source}}
	seen := map[string]bool{}
	categories := map[string]int{}
	for _, entry := range m.Cases {
		if !caseIDPattern.MatchString(entry.ID) || seen[entry.ID] {
			return nil, fmt.Errorf("invalid/duplicate case ID")
		}
		seen[entry.ID] = true
		prefix := "cases/" + entry.ID + "/"
		body, err := read(prefix + "case.json")
		if err != nil {
			return nil, err
		}
		if digest(body) != entry.Hash {
			return nil, fmt.Errorf("case hash mismatch: %s", entry.ID)
		}
		var e CaseEvidence
		if err = strictJSON(body, &e); err != nil {
			return nil, err
		}
		if e.ID != entry.ID || e.Version != "1.0.0" || e.Task == "" || len(e.Assertions) < 3 {
			return nil, fmt.Errorf("incomplete case: %s", entry.ID)
		}
		switch e.Category {
		case "boundary-string", "collection-sorting", "deterministic-transformation":
		default:
			return nil, fmt.Errorf("unknown category")
		}
		categories[e.Category]++
		source, err := read(prefix + "faulty.go.txt")
		if err != nil {
			return nil, err
		}
		reference, err := read(prefix + "reference.go.txt")
		if err != nil {
			return nil, err
		}
		if digest(source) != e.FixtureHash || digest(reference) != e.ReferenceHash {
			return nil, fmt.Errorf("fixture/reference hash mismatch: %s", e.ID)
		}
		e.Faulty = string(source)
		e.Reference = string(reference)
		e.CaseHash = entry.Hash
		if err = validateFixture(e, e.Faulty); err != nil {
			return nil, err
		}
		if err = validateFixture(e, e.Reference); err != nil {
			return nil, err
		}
		for _, a := range e.Assertions {
			if _, err = typedJSON(a.Input, e.InputType); err != nil {
				return nil, fmt.Errorf("%s input: %w", e.ID, err)
			}
			if _, err = typedJSON(a.Expected, e.OutputType); err != nil {
				return nil, fmt.Errorf("%s expected: %w", e.ID, err)
			}
		}
		d.cases = append(d.cases, e)
		expected, _ := json.Marshal(e.Assertions)
		// Artifact IDs remain empty until orchestration persists evidence through
		// modelstore. Embedded paths are logical assets, never store capabilities.
		d.revision.Cases = append(d.revision.Cases, modelstore.Case{ID: e.ID, Category: e.Category, FixtureHash: e.FixtureHash, Expected: string(expected)})
	}
	if len(categories) != 3 {
		return nil, fmt.Errorf("category count mismatch")
	}
	for _, n := range categories {
		if n != 4 {
			return nil, fmt.Errorf("category balance mismatch")
		}
	}
	return d, nil
}
func (d *Dataset) Revision() modelstore.DatasetRevision { return jsonCopy(d.revision) }
func (d *Dataset) Cases() []CaseRef {
	refs := make([]CaseRef, len(d.cases))
	for i, e := range d.cases {
		refs[i] = CaseRef{d.revision.ContentHash, e.ID, e.CaseHash}
	}
	return refs
}
func (r CaseRef) ID() string { return r.id }
func (d *Dataset) Evidence(ref CaseRef) (CaseEvidence, error) {
	if d == nil || ref.datasetHash != d.revision.ContentHash {
		return CaseEvidence{}, fmt.Errorf("untrusted dataset reference")
	}
	for _, e := range d.cases {
		if e.ID == ref.id && e.CaseHash == ref.caseHash {
			copy := e
			copy.Assertions = jsonCopy(e.Assertions)
			return copy, nil
		}
	}
	return CaseEvidence{}, fmt.Errorf("untrusted case reference")
}
func authoritative(ref CaseRef) (*Dataset, CaseEvidence, error) {
	d, err := LoadDataset(DatasetID)
	if err != nil {
		return nil, CaseEvidence{}, err
	}
	e, err := d.Evidence(ref)
	return d, e, err
}

var finiteTypes = map[string]reflect.Type{"bool": reflect.TypeOf(false), "string": reflect.TypeOf(""), "int": reflect.TypeOf(int64(0)), "int64": reflect.TypeOf(int64(0)), "float64": reflect.TypeOf(float64(0))}

func typedJSON(raw []byte, typ string) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("JSON must be UTF-8")
	}
	if len(raw) > evalrunner.MaxIOBytes {
		return nil, fmt.Errorf("JSON exceeds runner bound")
	}
	target, ok := finiteTypes[strings.TrimPrefix(typ, "[]")]
	if !ok {
		return nil, fmt.Errorf("unsupported finite type")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var shape any
	if err := dec.Decode(&shape); err != nil {
		return nil, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	if shape == nil {
		return nil, fmt.Errorf("null forbidden")
	}
	if strings.HasPrefix(typ, "[]") {
		values, ok := shape.([]any)
		if !ok {
			return nil, fmt.Errorf("expected array")
		}
		for _, v := range values {
			if v == nil {
				return nil, fmt.Errorf("null element")
			}
		}
		target = reflect.SliceOf(target)
	}
	value := reflect.New(target)
	if err := json.Unmarshal(raw, value.Interface()); err != nil {
		return nil, err
	}
	return json.Marshal(value.Elem().Interface())
}
func validateFixture(e CaseEvidence, source string) error {
	if len(source) == 0 || len(source) > evalrunner.MaxSourceBytes {
		return fmt.Errorf("fixture source bound")
	}
	f, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, parser.AllErrors)
	if err != nil {
		return err
	}
	if f.Name.Name != "main" || len(f.Imports) != 0 || len(f.Decls) != 1 {
		return fmt.Errorf("fixture must contain one function")
	}
	fn, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok || fn.Name.Name != e.Function || fn.Recv != nil || fn.Type.TypeParams != nil || fn.Body == nil {
		return fmt.Errorf("fixture function mismatch")
	}
	match := func(fields *ast.FieldList, typ string, input bool) bool {
		if fields == nil || len(fields.List) != 1 {
			return false
		}
		f := fields.List[0]
		if input && len(f.Names) != 1 || len(f.Names) > 1 {
			return false
		}
		node := f.Type
		if strings.HasPrefix(typ, "[]") {
			arr, ok := node.(*ast.ArrayType)
			if !ok || arr.Len != nil {
				return false
			}
			node = arr.Elt
			typ = typ[2:]
		}
		id, ok := node.(*ast.Ident)
		return ok && id.Name == typ
	}
	if _, ok := finiteTypes[strings.TrimPrefix(e.InputType, "[]")]; !ok {
		return fmt.Errorf("input type")
	}
	if _, ok := finiteTypes[strings.TrimPrefix(e.OutputType, "[]")]; !ok {
		return fmt.Errorf("output type")
	}
	if !match(fn.Type.Params, e.InputType, true) || !match(fn.Type.Results, e.OutputType, false) {
		return fmt.Errorf("fixture signature mismatch")
	}
	return nil
}
