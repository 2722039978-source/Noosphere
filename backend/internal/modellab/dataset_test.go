package modellab

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestDatasetHasTwelveCasesAndThreeCategories(t *testing.T) {
	d, err := LoadDataset("codetask-go-v1")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, ref := range d.Cases() {
		e, err := d.Evidence(ref)
		if err != nil {
			t.Fatal(err)
		}
		counts[e.Category]++
		if len(e.Assertions) < 3 || e.FixtureHash == "" || e.CaseHash == "" {
			t.Fatal("incomplete fixture")
		}
	}
	if len(d.Cases()) != 12 || len(counts) != 3 {
		t.Fatal(counts)
	}
	for _, n := range counts {
		if n != 4 {
			t.Fatal(counts)
		}
	}
	rev := d.Revision()
	rev.Cases[0].Expected = "forged"
	if d.Revision().Cases[0].Expected == "forged" {
		t.Fatal("mutable revision")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(rev.ID) {
		t.Fatal("revision ID incompatible with modelstore")
	}
	for _, c := range rev.Cases {
		if c.InputArtifact != "" || c.FixtureArtifact != "" {
			t.Fatal("logical embedded paths are not persisted artifact IDs")
		}
	}
	if _, err = LoadDataset("../codetask-go-v1"); err == nil {
		t.Fatal("unknown dataset accepted")
	}
	if _, err = d.Evidence(CaseRef{}); err == nil {
		t.Fatal("forged reference accepted")
	}
}

func TestPromptExcludesExpectedResults(t *testing.T) {
	d, err := LoadDataset("codetask-go-v1")
	if err != nil {
		t.Fatal(err)
	}
	ref := d.Cases()[0]
	e, _ := d.Evidence(ref)
	e.Task = "public repair task"
	e.Reference = "HIDDEN_REFERENCE_CANARY_79c4"
	e.Assertions = []Assertion{{Input: json.RawMessage(`"HIDDEN_INPUT_79c4"`), Expected: json.RawMessage(`"HIDDEN_EXPECTED_79c4"`)}}
	prompt := buildPrompt(e, PublicContext{Language: "go"})
	for _, secret := range []string{"HIDDEN_REFERENCE_CANARY_79c4", "HIDDEN_INPUT_79c4", "HIDDEN_EXPECTED_79c4"} {
		if strings.Contains(prompt, secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if !strings.Contains(prompt, e.Faulty) || !strings.Contains(prompt, e.Task) {
		t.Fatal("missing public repair data")
	}
}

func TestTypedJSONRejectsInvalidAndPreservesIntegers(t *testing.T) {
	for _, s := range []string{`null`, `1 2`, `1.2`, `9223372036854775808`, `"PASS"`, `{"pass":true}`} {
		if _, err := typedJSON([]byte(s), "int64"); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	a, err := typedJSON([]byte(`9007199254740993`), "int64")
	if err != nil || string(a) != "9007199254740993" {
		t.Fatalf("integer precision: %s %v", a, err)
	}
}
