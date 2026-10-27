package modellab

import (
	"math"
	"sort"
)

// Proportion counts independent selected cases, never repeated attempts/tokens.
type Proportion struct {
	Status    string   `json:"status"`
	Successes int      `json:"successes"`
	N         int      `json:"n"`
	Rate      *float64 `json:"rate"`
	Lower     *float64 `json:"lower_95"`
	Upper     *float64 `json:"upper_95"`
}

// Wilson95 uses the two-sided 95% Wilson score interval (NIST).
func Wilson95(successes, n int) Proportion {
	out := Proportion{Status: "insufficient", Successes: successes, N: n}
	if n <= 0 || successes < 0 || successes > n {
		return out
	}
	const z = 1.959963984540054
	nn := float64(n)
	p := float64(successes) / nn
	zz := z * z
	den := 1 + zz/nn
	center := (p + zz/(2*nn)) / den
	half := z * math.Sqrt(p*(1-p)/nn+zz/(4*nn*nn)) / den
	lo, hi := math.Max(0, center-half), math.Min(1, center+half)
	out.Status = "sufficient"
	out.Rate = &p
	out.Lower = &lo
	out.Upper = &hi
	return out
}

func median(v []float64) float64 {
	// The caller owns the slice.
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return v[n/2-1]/2 + v[n/2]/2
}
