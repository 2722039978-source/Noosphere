package modelstore

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func publicVersionManifest() Manifest {
	return Manifest{
		SchemaVersion: 1, EncoderVersion: EncoderVersion,
		WorkspaceID: "public-workspace", WorkflowID: "codelens.codetask.go.v1",
		Source: "offline_fixture", MemoryMode: "not_applicable",
		Stable:      ModelRevision{ID: "stable", GatewayModelID: "local", Provider: "ollama", ModelName: "example:latest", IdentityLevel: "digest", Digest: digest([]byte("revision-one"))},
		Candidate:   ModelRevision{ID: "candidate", GatewayModelID: "local", Provider: "ollama", ModelName: "example:latest", IdentityLevel: "digest", Digest: digest([]byte("revision-two"))},
		Profile:     InferenceProfile{ID: "profile"},
		Dataset:     DatasetRevision{ID: "dataset", BenchmarkID: "go", Version: "v1", ContentHash: digest([]byte("dataset")), ScorerVersion: "host-v1", Cases: []Case{{ID: "case", Category: "logic", Expected: "42"}}},
		Environment: EnvironmentSnapshot{ID: "environment", OS: "windows", Arch: "amd64", CollectorVersion: "v1"},
	}
}

func TestPublicVersionIdentityValidation(t *testing.T) {
	valid := publicVersionManifest().Stable
	for _, tc := range []struct {
		name string
		edit func(*ModelRevision)
	}{
		{"digest_missing", func(m *ModelRevision) { m.Digest = "" }},
		{"digest_malformed", func(m *ModelRevision) { m.Digest = "sha256:not-a-hash" }},
		{"declared_without_version", func(m *ModelRevision) { m.IdentityLevel = "declared"; m.ConfigHash = valid.Digest }},
		{"declared_without_config", func(m *ModelRevision) { m.IdentityLevel = "declared"; m.DeploymentVersion = "v1" }},
		{"unknown_without_reason", func(m *ModelRevision) { m.IdentityLevel = "unknown" }},
		{"endpoint_contains_credentials", func(m *ModelRevision) { m.EndpointFingerprint = "https://user:password@localhost" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := valid
			tc.edit(&model)
			if err := validateModelRevision(model); !errors.Is(err, ErrInvalid) {
				t.Fatalf("无效版本身份应被拒绝: %v", err)
			}
		})
	}
	for _, model := range []ModelRevision{
		valid,
		{ID: "declared", GatewayModelID: "local", Provider: "openai_compatible", ModelName: "example", IdentityLevel: "declared", DeploymentVersion: "v1", ConfigHash: valid.Digest},
		{ID: "unknown", GatewayModelID: "local", Provider: "openai_compatible", ModelName: "example", IdentityLevel: "unknown", UnknownReason: "服务未提供版本标识"},
	} {
		if err := validateModelRevision(model); err != nil {
			t.Fatalf("合法身份 %s: %v", model.ID, err)
		}
	}
}

func TestPublicVersionRegistryPreservesImmutableRevisions(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "versions.sqlite")
	s, err := Open(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m := publicVersionManifest()
	for _, revision := range []ModelRevision{m.Stable, m.Stable, m.Candidate} {
		if err := s.SaveModelRevision(ctx, revision); err != nil {
			t.Fatal(err)
		}
	}
	changed := m.Stable
	changed.Digest = m.Candidate.Digest
	if err := s.SaveModelRevision(ctx, changed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("重复 ID 不能替换已有版本: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []ModelRevision{m.Stable, m.Candidate} {
		got, err := s.GetModelRevision(ctx, want.ID)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("重新打开后版本改变: got=%+v want=%+v err=%v", got, want, err)
		}
	}
}

func TestPublicComparisonManifestBindsVersionsAndMode(t *testing.T) {
	m := publicVersionManifest()
	initial, initialHash, err := CanonicalManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	repeated, repeatedHash, err := CanonicalManifest(m)
	if err != nil || !bytes.Equal(initial, repeated) || initialHash != repeatedHash {
		t.Fatalf("相同输入的版本摘要不一致: %v", err)
	}
	m.Profile.Requested.Temperature = Float(0)
	explicit, explicitHash, err := CanonicalManifest(m)
	if err != nil || explicitHash == initialHash || !bytes.Contains(explicit, []byte(`"temperature":0`)) {
		t.Fatalf("显式零值必须保留在比较条件中: %v", err)
	}
	m = publicVersionManifest()
	m.Candidate.Digest = digest([]byte("revision-three"))
	if _, changedHash, err := CanonicalManifest(m); err != nil || changedHash == initialHash {
		t.Fatalf("模型版本变化必须改变比较摘要: %v", err)
	}
	for _, tc := range []struct {
		name  string
		mode  string
		same  bool
		valid bool
	}{
		{"compare_two_versions", "compare", false, true},
		{"implicit_two_versions", "", false, true},
		{"compare_same_version", "compare", true, false},
		{"implicit_same_version", "", true, false},
		{"baseline_same_version", "baseline", true, true},
		{"baseline_two_versions", "baseline", false, false},
		{"unknown_mode", "other", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := publicVersionManifest()
			manifest.EvaluationMode = tc.mode
			if tc.same {
				manifest.Candidate = manifest.Stable
			}
			_, _, err := CanonicalManifest(manifest)
			if (err == nil) != tc.valid || (err != nil && !errors.Is(err, ErrInvalid)) {
				t.Fatalf("比较模式校验: valid=%t err=%v", tc.valid, err)
			}
		})
	}
}
