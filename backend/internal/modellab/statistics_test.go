package modellab

import (
	"math"
	"testing"
)

func TestWilsonSmallSampleAndZeroTrials(t *testing.T) {
	a, b := Wilson95(12, 12), Wilson95(0, 12)
	if a.Status != "sufficient" || math.Abs(*a.Lower-0.7575059933) > 1e-8 || math.Abs(*b.Upper-0.2424940067) > 1e-8 {
		t.Fatalf("Wilson intervals: %+v %+v", a, b)
	}
	for _, c := range [][2]int{{0, 0}, {-1, 12}, {13, 12}} {
		v := Wilson95(c[0], c[1])
		if v.Status != "insufficient" || v.Lower != nil || v.Upper != nil || v.Rate != nil {
			t.Fatalf("invalid denominator: %+v", v)
		}
	}
}
