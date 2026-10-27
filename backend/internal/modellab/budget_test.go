package modellab

import (
	"github.com/noosphere/backend/internal/modelstore"
	"testing"
	"time"
)

func TestBudgetRejectsHiddenRetryAndOutOfBounds(t *testing.T) {
	b := DefaultBudget()
	p := testProfile()
	if err := b.validate(p); err != nil {
		t.Fatal(err)
	}
	p.Requested.RetryLimit = modelstore.Int(1)
	if b.validate(p) == nil {
		t.Fatal("hidden retry accepted")
	}
	p = testProfile()
	p.Requested.NumCtx = modelstore.Int(b.MaxContext + 1)
	if b.validate(p) == nil {
		t.Fatal("context over budget")
	}
	p = testProfile()
	p.Requested.DeadlineNS = modelstore.Int(int64(10 * time.Minute))
	if b.validate(p) == nil {
		t.Fatal("deadline over budget")
	}
}
