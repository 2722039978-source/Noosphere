package modellab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/noosphere/backend/internal/evalrunner"
	"github.com/noosphere/backend/internal/modelhttp"
	"github.com/noosphere/backend/internal/modelstore"
)

const DatasetWorkflowID = "codelens.codetask.go.v1"

// DatasetDocument contains only declarative finite Go tasks and host assertions.
// Source is never run at import time; generated answers still use the T1 runner.
type DatasetDocument struct {
	SchemaVersion int                   `json:"schema_version"`
	BenchmarkID   string                `json:"benchmark_id"`
	Version       string                `json:"version"`
	Title         string                `json:"title"`
	Description   string                `json:"description,omitempty"`
	Source        string                `json:"source,omitempty"`
	License       string                `json:"license,omitempty"`
	ScoringRule   string                `json:"scoring_rule"`
	Cases         []DatasetCaseDocument `json:"cases"`
}

type DatasetCaseDocument struct {
	NativeTools    *NativeToolCase `json:"native_tools,omitempty"`
	Reviewer       *ReviewerCase   `json:"reviewer,omitempty"`
	Rules          []TextRule      `json:"rules,omitempty"`
	ExpectedOutput json.RawMessage `json:"expected_output,omitempty"`
	ID             string          `json:"id"`
	Category       string          `json:"category"`
	Task           string          `json:"task"`
	Function       string          `json:"function"`
	InputType      string          `json:"input_type"`
	OutputType     string          `json:"output_type"`
	FaultySource   string          `json:"faulty_source"`
	Assertions     []Assertion     `json:"assertions"`
}

type DatasetSummary struct {
	Suite *SuiteMetadata `json:"suite,omitempty"`
	modelstore.DatasetRevision
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Archived    bool   `json:"archived"`
	CreatedAt   string `json:"created_at,omitempty"`
	WorkflowID  string `json:"workflow_id"`
	ScoringRule string `json:"scoring_rule"`
	ScorerHash  string `json:"scorer_sha256"`
}

type DatasetDetail struct {
	Revision     DatasetSummary  `json:"revision"`
	Document     DatasetDocument `json:"document"`
	DocumentJSON string          `json:"document_json"`
}

func datasetInputError(reason string) error {
	return fmt.Errorf("%w: %s", modelstore.ErrInvalid, reason)
}

func validateDatasetDocument(input DatasetDocument) (*Dataset, []byte, error) {
	if nativeToolsID(input.BenchmarkID) {
		return nil, nil, datasetInputError("reserved builtin native tools identity")
	}
	// Reject unsupported fields at transport decoding; normalize only finite
	// assertion values, leaving exact source and descriptive text hash-bound.
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > modelhttp.MaxBody {
		return nil, nil, datasetInputError("document must fit within 64 KiB")
	}
	if !utf8.ValidString(input.Title) || !utf8.ValidString(input.Description) || input.SchemaVersion != 1 || !caseIDPattern.MatchString(input.BenchmarkID) || input.BenchmarkID == DatasetID || input.BenchmarkID == TextDatasetID || reviewerID(input.BenchmarkID) != "" || !modelhttp.ID(input.Version) || len(input.Version) > 64 || strings.TrimSpace(input.Title) == "" || len(input.Title) > 200 || len(input.Description) > 4000 || len(input.Source) > 1000 || len(input.License) > 128 || input.ScoringRule != "exact" || len(input.Cases) < 1 || len(input.Cases) > 50 {
		return nil, nil, datasetInputError("dataset schema, identity, exact scoring, or 1..50 case limit")
	}
	var doc DatasetDocument
	if err = strictJSON(raw, &doc); err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	d := &Dataset{scorerHash: digest(scorerSource)}
	for i, c := range doc.Cases {
		if c.NativeTools != nil {
			return nil, nil, datasetInputError("native tool cases are immutable builtins")
		}
		if !caseIDPattern.MatchString(c.ID) || seen[c.ID] || strings.TrimSpace(c.Category) == "" || len(c.Category) > 80 || strings.TrimSpace(c.Task) == "" || len(c.Task) > 8000 || len(c.Assertions) < 1 || len(c.Assertions) > 100 || len(c.Rules) > 0 || len(c.ExpectedOutput) > 0 || c.Reviewer != nil {
			return nil, nil, datasetInputError(fmt.Sprintf("invalid case %d identity, task, or 1..100 assertion limit", i+1))
		}
		seen[c.ID] = true
		if err = evalrunner.ValidateCompileSpec(evalrunner.CompileSpec{Source: c.FaultySource, FunctionName: c.Function, InputType: c.InputType, OutputType: c.OutputType}); err != nil {
			return nil, nil, datasetInputError(fmt.Sprintf("case %s: %v", c.ID, err))
		}
		for j, a := range c.Assertions {
			doc.Cases[i].Assertions[j].Input, err = typedJSON(a.Input, c.InputType)
			if err != nil {
				return nil, nil, datasetInputError(fmt.Sprintf("case %s assertion %d input: %v", c.ID, j+1, err))
			}
			doc.Cases[i].Assertions[j].Expected, err = typedJSON(a.Expected, c.OutputType)
			if err != nil {
				return nil, nil, datasetInputError(fmt.Sprintf("case %s assertion %d expected: %v", c.ID, j+1, err))
			}
		}
		e := CaseEvidence{ID: c.ID, Version: doc.Version, Category: c.Category, Task: c.Task, Function: c.Function, InputType: c.InputType, OutputType: c.OutputType, FixtureHash: digest([]byte(c.FaultySource)), Faulty: c.FaultySource, Assertions: doc.Cases[i].Assertions}
		body, _ := json.Marshal(e)
		e.CaseHash = digest(body)
		d.cases = append(d.cases, e)
	}
	canonical, err := json.Marshal(doc)
	if err != nil || len(canonical) > modelhttp.MaxBody {
		return nil, nil, datasetInputError("canonical document exceeds 64 KiB")
	}
	hash := digest(canonical)
	d.revision = modelstore.DatasetRevision{ID: "custom." + hash[:32], BenchmarkID: doc.BenchmarkID, Version: doc.Version, ContentHash: hash, SourceRevisionID: "custom." + hash[:32], SourceContentHash: hash, ScorerVersion: ScorerVersion, RunnerVersion: "evalrunner-t1-wasi-v1", PromptTemplateHash: digest([]byte(promptTemplate)), License: doc.License, Source: doc.Source}
	d.revision.ScorerHash = d.scorerHash
	for _, e := range d.cases {
		expected, _ := json.Marshal(e.Assertions)
		d.revision.Cases = append(d.revision.Cases, modelstore.Case{ID: e.ID, Category: e.Category, FixtureHash: e.FixtureHash, Expected: string(expected)})
	}
	return d, canonical, nil
}

func definitionDataset(record modelstore.DatasetDefinition) (*Dataset, DatasetDocument, error) {
	var doc DatasetDocument
	if err := strictJSON(record.Document, &doc); err != nil {
		return nil, doc, err
	}
	d, canonical, err := validateDatasetDocument(doc)
	if err != nil {
		return nil, doc, err
	}
	if !bytes.Equal(canonical, record.Document) || digest(canonical) != record.ContentHash || record.BenchmarkID != doc.BenchmarkID || record.Version != doc.Version || record.ID != customDatasetID(record.WorkspaceID, record.ContentHash) {
		return nil, doc, fmt.Errorf("%w: dataset definition integrity mismatch", modelstore.ErrInsufficientEvidence)
	}
	d.revision.ID = record.ID
	d.revision.SourceRevisionID = record.ID
	return d, doc, nil
}

func customDatasetID(workspace, hash string) string {
	return "custom." + digest([]byte(workspace))[:16] + "." + hash[:32]
}

func datasetDetail(d *Dataset, doc DatasetDocument, record modelstore.DatasetDefinition) DatasetDetail {
	summary := DatasetSummary{DatasetRevision: d.Revision(), Kind: "workspace_custom", Title: doc.Title, Description: doc.Description, WorkspaceID: record.WorkspaceID, Archived: record.Archived, CreatedAt: record.CreatedAt, WorkflowID: DatasetWorkflowID, ScoringRule: "exact", ScorerHash: d.scorerHash}
	raw, _ := json.Marshal(doc)
	return DatasetDetail{Revision: summary, Document: doc, DocumentJSON: string(raw)}
}

func builtinDatasetDetail() (DatasetDetail, error) {
	d, err := LoadDataset(DatasetID)
	if err != nil {
		return DatasetDetail{}, err
	}
	doc := DatasetDocument{SchemaVersion: 1, BenchmarkID: DatasetID, Version: d.revision.Version, Title: "CodeTask Go v1", Source: d.revision.Source, License: d.revision.License, ScoringRule: "exact", Cases: []DatasetCaseDocument{}}
	for _, e := range d.cases {
		doc.Cases = append(doc.Cases, DatasetCaseDocument{ID: e.ID, Category: e.Category, Task: e.Task, Function: e.Function, InputType: e.InputType, OutputType: e.OutputType, FaultySource: e.Faulty, Assertions: jsonCopy(e.Assertions)})
	}
	detail := datasetDetail(d, doc, modelstore.DatasetDefinition{})
	detail.Revision.Kind = "builtin"
	return detail, nil
}

func builtinTextDatasetDetail() (DatasetDetail, error) {
	d, err := LoadDataset(TextDatasetID)
	if err != nil {
		return DatasetDetail{}, err
	}
	var meta textDatasetManifest
	if err := strictJSON(textDatasetSource, &meta); err != nil {
		return DatasetDetail{}, err
	}
	doc := DatasetDocument{SchemaVersion: 1, BenchmarkID: TextDatasetID, Version: d.revision.Version, Title: meta.Title, Description: meta.Description, Source: d.revision.Source, License: d.revision.License, ScoringRule: textScoringRule, Cases: []DatasetCaseDocument{}}
	for _, e := range d.cases {
		doc.Cases = append(doc.Cases, DatasetCaseDocument{ID: e.ID, Category: e.Category, Task: e.Task, Rules: jsonCopy(e.Rules), ExpectedOutput: append(json.RawMessage(nil), e.ExpectedOutput...)})
	}
	detail := datasetDetail(d, doc, modelstore.DatasetDefinition{})
	detail.Revision.Kind, detail.Revision.WorkflowID, detail.Revision.ScoringRule = "builtin", TextWorkflowID, textScoringRule
	detail.Revision.Suite = textSuiteMetadata()
	return detail, nil
}

func (s *Service) SaveDataset(ctx context.Context, workspaceID string, doc DatasetDocument) (DatasetDetail, error) {
	if reviewerID(doc.BenchmarkID) != "" {
		return DatasetDetail{}, datasetInputError("reserved built-in reviewer identity")
	}
	if !modelhttp.ID(workspaceID) {
		return DatasetDetail{}, modelstore.ErrInvalid
	}
	d, raw, err := validateDatasetDocument(doc)
	if err != nil {
		return DatasetDetail{}, err
	}
	record, err := s.store.SaveDatasetDefinition(ctx, modelstore.DatasetDefinition{ID: customDatasetID(workspaceID, d.revision.ContentHash), WorkspaceID: workspaceID, BenchmarkID: doc.BenchmarkID, Version: doc.Version, ContentHash: d.revision.ContentHash, Document: raw})
	if err != nil {
		return DatasetDetail{}, err
	}
	return s.GetDataset(ctx, workspaceID, record.ID)
}

func (s *Service) GetDataset(ctx context.Context, workspaceID, id string) (DatasetDetail, error) {
	if nativeToolsID(id) {
		return builtinNativeToolsDetail()
	}
	if reviewerID(id) != "" {
		return builtinReviewerDetail(id)
	}
	if id == TextDatasetID || id == TextDatasetID+".1.0.0" {
		return builtinTextDatasetDetail()
	}
	if id == DatasetID || id == DatasetID+".1.0.0" {
		return builtinDatasetDetail()
	}
	record, err := s.store.GetDatasetDefinition(ctx, workspaceID, id)
	if err != nil {
		return DatasetDetail{}, err
	}
	d, doc, err := definitionDataset(record)
	if err != nil {
		return DatasetDetail{}, err
	}
	return datasetDetail(d, doc, record), nil
}

func (s *Service) ListDatasets(ctx context.Context, workspaceID string, includeArchived bool) ([]DatasetSummary, error) {
	builtin, err := builtinDatasetDetail()
	if err != nil {
		return nil, err
	}
	out := []DatasetSummary{catalogueSummary(builtin.Revision)}
	textBuiltin, err := builtinTextDatasetDetail()
	if err != nil {
		return nil, err
	}
	out = append(out, catalogueSummary(textBuiltin.Revision))
	for _, id := range reviewerIDs {
		detail, err := builtinReviewerDetail(id)
		if err != nil {
			return nil, err
		}
		out = append(out, catalogueSummary(detail.Revision))
	}
	if workspaceID == "" {
		tools, err := builtinNativeToolsDetail()
		if err != nil {
			return nil, err
		}
		out = append(out, catalogueSummary(tools.Revision))
		return out, nil
	}
	tools, err := builtinNativeToolsDetail()
	if err != nil {
		return nil, err
	}
	out = append(out, catalogueSummary(tools.Revision))
	records, err := s.store.ListDatasetDefinitions(ctx, workspaceID, includeArchived)
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		d, doc, err := definitionDataset(r)
		if err != nil {
			return nil, err
		}
		out = append(out, catalogueSummary(datasetDetail(d, doc, r).Revision))
	}
	return out, nil
}

// Assertion documents can occupy most of the 64 KiB source budget. Catalogue
// reads carry case identities; the detail endpoint retains complete evidence.
func catalogueSummary(summary DatasetSummary) DatasetSummary {
	for i := range summary.Cases {
		summary.Cases[i].Expected = ""
	}
	return summary
}

func (s *Service) ArchiveDataset(ctx context.Context, workspaceID, id string, archived bool) (DatasetSummary, error) {
	record, err := s.store.ArchiveDatasetDefinition(ctx, workspaceID, id, archived)
	if err != nil {
		return DatasetSummary{}, err
	}
	d, doc, err := definitionDataset(record)
	if err != nil {
		return DatasetSummary{}, err
	}
	return datasetDetail(d, doc, record).Revision, nil
}

func (s *Service) resolveDataset(ctx context.Context, workspaceID, id string, allowArchived bool) (*Dataset, error) {
	if nativeToolsID(id) {
		return loadNativeToolsDataset()
	}
	if reviewerID(id) != "" {
		return LoadDataset(reviewerID(id))
	}
	if id == TextDatasetID || id == TextDatasetID+".1.0.0" {
		return LoadDataset(TextDatasetID)
	}
	if id == DatasetID || id == DatasetID+".1.0.0" {
		return LoadDataset(DatasetID)
	}
	record, err := s.store.GetDatasetDefinition(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if record.Archived && !allowArchived {
		return nil, fmt.Errorf("%w: dataset revision archived", modelstore.ErrRevisionConflict)
	}
	d, _, err := definitionDataset(record)
	return d, err
}

func (s *Service) frozenDataset(ctx context.Context, e modelstore.Evaluation) (*Dataset, error) {
	revision := e.Manifest.Dataset
	if revision.SourceRevisionID == "" {
		return LoadDataset(revision.BenchmarkID)
	}
	d, err := s.resolveDataset(ctx, e.Manifest.WorkspaceID, revision.SourceRevisionID, true)
	if err != nil {
		return nil, err
	}
	if revision.SourceContentHash != d.revision.ContentHash || revision.BenchmarkID != d.revision.BenchmarkID || revision.Version != d.revision.Version {
		return nil, fmt.Errorf("%w: frozen source revision mismatch", modelstore.ErrInsufficientEvidence)
	}
	return d, nil
}
