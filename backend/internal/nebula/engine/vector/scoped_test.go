package vector

import (
	"fmt"
	"testing"
)

func TestScopedRecallBeyondGlobalCandidateBudget(t *testing.T) {
	h := NewHNSW(4, 5)
	for i := 0; i < 60; i++ {
		h.Insert(fmt.Sprintf("other:%d", i), []float32{1, 0})
	}
	h.Insert("selected:only", []float32{0.9, 0.1})
	hits := h.SearchScoped([]float32{1, 0}, 3, "selected:")
	if len(hits) != 1 || hits[0].Key != "selected:only" {
		t.Fatalf("lost scoped semantic match: %+v", hits)
	}
}
