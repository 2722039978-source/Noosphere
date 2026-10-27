package modellab

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/noosphere/backend/internal/codelens"
)

// ContextBuilder owns only benchmark fixtures. It never indexes projects or
// writes Nebula. The Adapter is started/stopped by its existing lifecycle owner.
type ContextBuilder struct {
	adapter *codelens.Adapter
	root    string
	mu      sync.Mutex
	closed  bool
	version string
	owned   map[string]os.FileInfo
}

func NewContextBuilder(adapter *codelens.Adapter, tempParent string) (*ContextBuilder, error) {
	if adapter == nil {
		return nil, fmt.Errorf("CodeLens adapter required")
	}
	root, err := os.MkdirTemp(tempParent, "codetask-fixtures-")
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &ContextBuilder{adapter: adapter, root: root, owned: make(map[string]os.FileInfo)}, nil
}
func (b *ContextBuilder) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return os.RemoveAll(b.root)
}

// PublicContext contains allowlisted service facts and host-bound source
// evidence. File is semantic/relative; random machine paths stay out of prompts.
type PublicContext struct {
	File            string          `json:"file"`
	Language        string          `json:"language"`
	Lines           int             `json:"lines"`
	SizeBytes       int             `json:"size_bytes"`
	Symbol          codelens.Entity `json:"symbol"`
	CodeLensVersion string          `json:"codelens_version"`
	Parser          string          `json:"parser"`
	HostFixtureHash string          `json:"host_fixture_sha256"`
}
type FrozenContext struct {
	Public               PublicContext `json:"public"`
	ContextHash          string        `json:"context_hash"`
	Prompt               string        `json:"prompt"`
	PromptHash           string        `json:"prompt_hash"`
	PromptVersion        string        `json:"prompt_version"`
	PromptTemplateHash   string        `json:"prompt_template_hash"`
	DatasetHash          string        `json:"dataset_hash"`
	CaseHash             string        `json:"case_hash"`
	AnalysisResponseHash string        `json:"analysis_response_hash"`
	HealthResponseHash   string        `json:"health_response_hash"`
	// Raw protocol responses retain their original path and exact bytes, separate
	// from reproducible public context. They do not assert server source hashing.
	AnalysisResponse json.RawMessage `json:"analysis_response"`
	HealthResponse   json.RawMessage `json:"health_response"`
}

func buildPrompt(e CaseEvidence, p PublicContext) string {
	raw, _ := json.Marshal(p)
	return promptTemplate + "Task: " + e.Task + "\nSignature: func " + e.Function + "(input " + e.InputType + ") " + e.OutputType + "\nFaulty source:\n" + e.Faulty + "\nCodeLens evidence:\n" + string(raw) + "\n"
}

func (b *ContextBuilder) BuildContext(ctx context.Context, dataset *Dataset, ref CaseRef) (FrozenContext, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out FrozenContext
	if b.closed {
		return out, fmt.Errorf("context builder closed")
	}
	// Resolve authoritative bytes again, even if a caller mutates its audit DTO.
	trusted, e, err := authoritative(ref)
	if err != nil {
		return out, err
	}
	if dataset == nil || dataset.revision.ContentHash != trusted.revision.ContentHash {
		return out, fmt.Errorf("dataset revision mismatch")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	file := filepath.Join(b.root, e.ID+".go")
	if err = confinedFixture(b.root, file, false); err != nil {
		return out, err
	}
	if info, statErr := os.Lstat(file); os.IsNotExist(statErr) {
		f, createErr := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr != nil {
			return out, createErr
		}
		_, writeErr := f.Write([]byte(e.Faulty))
		closeErr := f.Close()
		if writeErr != nil {
			return out, writeErr
		}
		if closeErr != nil {
			return out, closeErr
		}
		info, err = os.Lstat(file)
		if err != nil {
			return out, err
		}
		b.owned[file] = info
	} else if statErr != nil {
		return out, statErr
	} else if prior, ok := b.owned[file]; !ok || !os.SameFile(prior, info) {
		return out, fmt.Errorf("foreign/replaced fixture file")
	}
	before, err := checkedFixture(b.root, file, e.FixtureHash)
	if err != nil {
		return out, err
	}
	health, healthRaw, err := b.adapter.HealthEvidence(ctx)
	if err != nil {
		return out, fmt.Errorf("CodeLens health: %w", err)
	}
	if health.Status != "ok" || health.Service != "codelens" || health.Version != "0.6.0" || health.Parser != "tree-sitter" {
		return out, fmt.Errorf("unsupported CodeLens health evidence")
	}
	goSupported := false
	for _, lang := range health.Languages {
		goSupported = goSupported || lang == "go"
	}
	if !goSupported {
		return out, fmt.Errorf("CodeLens Go parser unavailable")
	}
	if b.version != "" && b.version != health.Version {
		return out, fmt.Errorf("CodeLens version drift")
	}
	analysis, raw, err := b.adapter.AnalyzeFile(ctx, b.root, filepath.Base(file))
	if err != nil {
		return out, fmt.Errorf("CodeLens analyze: %w", err)
	}
	after, err := checkedFixture(b.root, file, e.FixtureHash)
	if err != nil {
		return out, err
	}
	if string(before) != string(after) {
		return out, fmt.Errorf("fixture changed during analysis")
	}
	info, err := os.Lstat(file)
	if err != nil || !os.SameFile(b.owned[file], info) {
		return out, fmt.Errorf("fixture replaced during analysis")
	}
	symbol, err := validateAnalysis(e, file, before, analysis)
	if err != nil {
		return out, err
	}
	b.version = health.Version
	out.Public = PublicContext{File: "cases/" + e.ID + "/faulty.go", Language: analysis.Language, Lines: analysis.Lines, SizeBytes: analysis.SizeBytes, Symbol: symbol, CodeLensVersion: health.Version, Parser: health.Parser, HostFixtureHash: e.FixtureHash}
	publicRaw, _ := json.Marshal(out.Public)
	out.ContextHash = digest(publicRaw)
	out.Prompt = buildPrompt(e, out.Public)
	out.PromptHash = digest([]byte(out.Prompt))
	out.PromptVersion = PromptVersion
	out.PromptTemplateHash = trusted.revision.PromptTemplateHash
	out.DatasetHash = trusted.revision.ContentHash
	out.CaseHash = e.CaseHash
	out.AnalysisResponseHash = digest(raw)
	out.HealthResponseHash = digest(healthRaw)
	out.AnalysisResponse = append([]byte(nil), raw...)
	out.HealthResponse = append([]byte(nil), healthRaw...)
	return out, nil
}
func confinedFixture(root, file string, mustExist bool) error {
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("fixture escapes owned root")
	}
	// Lstat every component, including ancestors: links/junctions are rejected,
	// even when their resolved destination would be inside the root.
	for path := file; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			if path == file && !mustExist && os.IsNotExist(err) {
			} else {
				return err
			}
		} else {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("fixture link/junction forbidden")
			}
			if path == file && !info.Mode().IsRegular() {
				return fmt.Errorf("fixture is not a regular file")
			}
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	return nil
}
func checkedFixture(root, file, hash string) ([]byte, error) {
	if err := confinedFixture(root, file, true); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if digest(raw) != hash {
		return nil, fmt.Errorf("fixture hash mismatch")
	}
	return raw, nil
}
func validateAnalysis(e CaseEvidence, path string, raw []byte, a codelens.Analysis) (codelens.Entity, error) {
	var zero codelens.Entity
	if filepath.Clean(a.File) != filepath.Clean(path) || a.Language != "go" || a.SizeBytes != len(raw) || a.Lines != strings.Count(strings.TrimSuffix(string(raw), "\n"), "\n")+1 || len(a.Imports) != 0 {
		return zero, fmt.Errorf("CodeLens source/language/size mismatch")
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, path, raw, parser.AllErrors)
	if err != nil {
		return zero, err
	}
	fn, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		return zero, fmt.Errorf("fixture target missing")
	}
	start, end := fs.Position(fn.Pos()), fs.Position(fn.End())
	found := 0
	var selected codelens.Entity
	for _, s := range a.Entities {
		if s.Type != "function" {
			continue
		}
		if s.Name != e.Function || s.Scope != "" || s.Line != start.Line || s.EndLine != end.Line || s.StartByte != start.Offset || s.EndByte != end.Offset {
			return zero, fmt.Errorf("CodeLens function/range mismatch")
		}
		selected = s
		found++
	}
	if found != 1 {
		return zero, fmt.Errorf("CodeLens target missing/ambiguous")
	}
	return selected, nil
}
