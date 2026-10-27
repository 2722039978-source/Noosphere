package modelstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// RuleValue is a strict scalar union. Exactly one field must be set. Unknown
// marshals to JSON null and is reserved for an unobserved/insufficient result;
// thresholds must always have a concrete number, boolean or string value.
type RuleValue struct {
	Number  *float64
	Boolean *bool
	Text    *string
	Unknown bool
}

func NumberValue(v float64) RuleValue { return RuleValue{Number: &v} }
func BoolValue(v bool) RuleValue      { return RuleValue{Boolean: &v} }
func StringValue(v string) RuleValue  { return RuleValue{Text: &v} }
func UnknownValue() RuleValue         { return RuleValue{Unknown: true} }
func (v RuleValue) kind() (string, error) {
	n := 0
	kind := ""
	if v.Number != nil {
		n++
		kind = "number"
		if math.IsNaN(*v.Number) || math.IsInf(*v.Number, 0) {
			return "", ErrInvalid
		}
	}
	if v.Boolean != nil {
		n++
		kind = "boolean"
	}
	if v.Text != nil {
		n++
		kind = "string"
		if len(*v.Text) > 1024 {
			return "", ErrInvalid
		}
	}
	if v.Unknown {
		n++
		kind = "unknown"
	}
	if n != 1 {
		return "", fmt.Errorf("%w: rule value requires exactly one scalar", ErrInvalid)
	}
	return kind, nil
}
func (v RuleValue) MarshalJSON() ([]byte, error) {
	k, err := v.kind()
	if err != nil {
		return nil, err
	}
	switch k {
	case "number":
		return json.Marshal(*v.Number)
	case "boolean":
		return json.Marshal(*v.Boolean)
	case "string":
		return json.Marshal(*v.Text)
	default:
		return []byte("null"), nil
	}
}
func (v *RuleValue) UnmarshalJSON(b []byte) error {
	var raw any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&raw); err != nil {
		return err
	}
	switch x := raw.(type) {
	case nil:
		*v = UnknownValue()
	case bool:
		*v = BoolValue(x)
	case string:
		*v = StringValue(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return fmt.Errorf("%w: numeric rule value", ErrInvalid)
		}
		*v = NumberValue(f)
	default:
		return fmt.Errorf("%w: rule value must be scalar", ErrInvalid)
	}
	_, err := v.kind()
	return err
}

// PolicyRule freezes both the threshold and the independent sampling/window
// requirement. Stage is an active release state or "all". All matching release
// rules are mandatory for advance/resume. No formula is evaluated in this layer.
type PolicyRule struct {
	ID                  string    `json:"id"`
	Domain              string    `json:"domain"`
	Stage               string    `json:"stage"`
	Operator            string    `json:"operator"`
	Required            RuleValue `json:"required"`
	Unit                string    `json:"unit"`
	MinimumSamples      int64     `json:"minimum_samples"`
	ObservationWindowNS int64     `json:"observation_window_ns"`
}
type RuleResult struct {
	ID                  string    `json:"id"`
	Observed            RuleValue `json:"observed"`
	Required            RuleValue `json:"required"`
	Unit                string    `json:"unit"`
	EvidenceIDs         []string  `json:"evidence_ids"`
	Status              string    `json:"status"`
	Reason              string    `json:"reason"`
	SampleCount         int64     `json:"sample_count"`
	ObservationWindowNS int64     `json:"observation_window_ns"`
}

func validPolicyRule(r PolicyRule) error {
	if !validID(r.ID) || strings.TrimSpace(r.Unit) == "" || len(r.Unit) > 128 || r.MinimumSamples < 0 || r.ObservationWindowNS < 0 {
		return ErrInvalid
	}
	switch r.Domain {
	case "quality", "statistics", "resources", "identity", "isolation", "simulation":
	default:
		return ErrInvalid
	}
	switch r.Stage {
	case "all", "OFFLINE_EVAL", "SHADOW", "CANARY_5", "CANARY_25", "CANARY_50", "PROMOTED":
	default:
		return ErrInvalid
	}
	kind, err := r.Required.kind()
	if err != nil || kind == "unknown" {
		return ErrInvalid
	}
	switch r.Operator {
	case "eq", "ne":
	case "lt", "lte", "gt", "gte":
		if kind != "number" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func validatePolicy(p ReleasePolicy) error {
	if !validID(p.ID) || p.SchemaVersion != 1 || p.EncoderVersion != EncoderVersion || p.Mode != "simulation" || strings.TrimSpace(p.Version) == "" || p.RollbackRetentionNS <= 0 || len(p.ReleaseRules) == 0 || len(p.RollbackRules) == 0 {
		return fmt.Errorf("%w: policy needs frozen rules and rollback retention", ErrInvalid)
	}
	domains := map[string]bool{}
	seen := map[string]bool{}
	for _, r := range p.ReleaseRules {
		if err := validPolicyRule(r); err != nil {
			return err
		}
		if seen[r.ID] {
			return ErrInvalid
		}
		seen[r.ID] = true
		domains[r.Domain] = true
	}
	for _, r := range p.RollbackRules {
		if err := validPolicyRule(r); err != nil {
			return err
		}
		if seen[r.ID] {
			return ErrInvalid
		}
		seen[r.ID] = true
	}
	for _, domain := range []string{"quality", "statistics", "resources"} {
		if !domains[domain] {
			return fmt.Errorf("%w: missing frozen %s rule", ErrInvalid, domain)
		}
	}
	return nil
}
func getPolicy(ctx context.Context, q queryer, id string) (ReleasePolicy, error) {
	var p ReleasePolicy
	var body, hash string
	err := q.QueryRowContext(ctx, "SELECT body_json,policy_hash FROM release_policies WHERE id=?", id).Scan(&body, &hash)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if digest([]byte(body)) != hash {
		return p, ErrInsufficientEvidence
	}
	var w struct {
		SchemaVersion  int           `json:"schema_version"`
		EncoderVersion string        `json:"encoder_version"`
		Payload        ReleasePolicy `json:"payload"`
	}
	d := json.NewDecoder(strings.NewReader(body))
	d.DisallowUnknownFields()
	if err = d.Decode(&w); err != nil {
		return p, err
	}
	if w.SchemaVersion != 1 || w.EncoderVersion != EncoderVersion {
		return p, ErrInvalid
	}
	p = w.Payload
	if p.ID != id {
		return p, ErrInvalid
	}
	if err = validatePolicy(p); err != nil {
		return p, err
	}
	p.Hash = hash
	return p, nil
}

// GetPolicy reads and validates the complete immutable rule/rollback snapshot.
// Old sparse snapshots remain in history but cannot authorize a new advance.
func (s *Store) GetPolicy(ctx context.Context, id string) (ReleasePolicy, error) {
	return getPolicy(ctx, s.db, id)
}
func scalarEqual(a, b RuleValue) bool {
	aa, _, err := canonical(a)
	if err != nil {
		return false
	}
	bb, _, err := canonical(b)
	return err == nil && bytes.Equal(aa, bb)
}
func validateRuleResult(result RuleResult, rule PolicyRule) error {
	if result.ID != rule.ID || result.Unit != rule.Unit || !scalarEqual(result.Required, rule.Required) || result.SampleCount < 0 || result.ObservationWindowNS < 0 || strings.TrimSpace(result.Reason) == "" {
		return ErrInvalid
	}
	kind, err := result.Observed.kind()
	if err != nil {
		return err
	}
	requiredKind, _ := rule.Required.kind()
	if kind != "unknown" && kind != requiredKind {
		return ErrInvalid
	}
	switch result.Status {
	case "pass", "fail", "insufficient", "not_applicable":
	default:
		return ErrInvalid
	}
	if kind == "unknown" && result.Status != "insufficient" && result.Status != "not_applicable" {
		return ErrInvalid
	}
	for _, id := range result.EvidenceIDs {
		if !validID(id) {
			return ErrInvalid
		}
	}
	return nil
}

// validateGateDecision verifies the shape and frozen provenance of a service's
// decision. It deliberately does not recompute observed values or comparisons.
func validateGateDecision(p ReleasePolicy, d GateDecision, stage string) error {
	if !d.Allowed || d.PolicyHash != p.Hash {
		return ErrInsufficientEvidence
	}
	rules := map[string]PolicyRule{}
	for _, r := range p.ReleaseRules {
		if r.Stage == "all" || r.Stage == stage {
			rules[r.ID] = r
		}
	}
	if len(rules) == 0 || len(d.Rules) != len(rules) {
		return ErrInsufficientEvidence
	}
	seen := map[string]bool{}
	for _, result := range d.Rules {
		rule, ok := rules[result.ID]
		if !ok || seen[result.ID] {
			return ErrInsufficientEvidence
		}
		seen[result.ID] = true
		if err := validateRuleResult(result, rule); err != nil {
			return ErrInsufficientEvidence
		}
		if result.Status != "pass" || len(result.EvidenceIDs) == 0 || result.SampleCount < rule.MinimumSamples || result.ObservationWindowNS < rule.ObservationWindowNS {
			return ErrInsufficientEvidence
		}
	}
	return nil
}
