package service_test

import (
	"testing"

	"github.com/noosphere/backend/internal/nebula/service"
)

func TestExtractorClassifyFallback(t *testing.T) {
	ext := service.NewExtractor(nil) // nil -> fallback keyword classification

	tests := []struct {
		content  string
		tags     []string
		wantType service.MemoryType
	}{
		{"How to bake a cake step by step", nil, service.Procedural},
		{"The definition of entropy is a measure of disorder", nil, service.Semantic},
		{"TODO: finish the report by Friday", nil, service.Working},
		{"Yesterday I had a meeting with the team", nil, service.Episodic},
		{"This is a fact about physics: E=mc^2", nil, service.Semantic},
		{"Step 1: Install the package. Step 2: Run setup.", nil, service.Procedural},
		{"Meeting happened today at 3pm", nil, service.Episodic},
		{"simple note", nil, service.Working},
	}

	for _, tc := range tests {
		result := ext.Classify(tc.content, tc.tags)
		if result.Type != tc.wantType {
			t.Errorf("Classify(%q) = %s, want %s", tc.content, result.Type, tc.wantType)
		}
		if result.Importance < 0 || result.Importance > 1 {
			t.Errorf("Classify(%q) importance=%.2f, want 0.0-1.0", tc.content, result.Importance)
		}
		if result.Summary == "" {
			t.Errorf("Classify(%q) summary is empty", tc.content)
		}
	}
}

func TestExtractorTagOverride(t *testing.T) {
	ext := service.NewExtractor(nil)

	// Tag should override content-based classification
	result := ext.Classify("random text", []string{"procedural"})
	if result.Type != service.Procedural {
		t.Errorf("tag override: got %s, want procedural", result.Type)
	}

	result = ext.Classify("random text", []string{"knowledge"})
	if result.Type != service.Semantic {
		t.Errorf("tag override: got %s, want semantic", result.Type)
	}
}

func TestExtractorValidOutput(t *testing.T) {
	// With a nil embedder, Classify should always produce valid output
	// (non-empty type, importance in [0,1], non-empty summary).
	ext := service.NewExtractor(nil)

	inputs := []string{
		"Short text",
		"A longer piece of content that should still produce valid classification output with reasonable importance",
		"Step by step guide to doing something",
		"",
	}

	for _, content := range inputs {
		result := ext.Classify(content, nil)
		if result.Type == "" {
			t.Errorf("Classify(%q) returned empty type", content)
		}
		if result.Importance < 0 || result.Importance > 1 {
			t.Errorf("Classify(%q) importance=%.2f out of range [0,1]", content, result.Importance)
		}
		// Summary can be empty for empty content
		if content != "" && result.Summary == "" {
			t.Errorf("Classify(%q) returned empty summary", content)
		}
	}
}
