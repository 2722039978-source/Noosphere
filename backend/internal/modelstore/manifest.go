package modelstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const EncoderVersion = "canonical-json-v1"

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validID(s string) bool  { return safeID.MatchString(s) }
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func canonical(v any) ([]byte, string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, "", e
	}
	var normalized any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e = d.Decode(&normalized); e != nil {
		return nil, "", e
	}
	b, e = json.Marshal(normalizeNumbers(normalized))
	if e != nil {
		return nil, "", e
	}
	return b, digest(b), nil
}

// DecodeManifest rejects unknown fields recursively, including credentials, instead
// of accepting a generic provider/config map and silently dropping secrets.
func DecodeManifest(b []byte) (Manifest, error) {
	m, err := decodeManifestRecord(b)
	if err != nil {
		return m, err
	}
	_, _, err = CanonicalManifest(m)
	return m, err
}
func decodeManifestRecord(b []byte) (Manifest, error) {
	var m Manifest
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&m); e != nil {
		return m, fmt.Errorf("%w: %v", ErrInvalid, e)
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return m, fmt.Errorf("%w: trailing JSON", ErrInvalid)
	}
	return m, nil
}
func CanonicalManifest(m Manifest) ([]byte, string, error) {
	if m.SchemaVersion != 1 || m.EncoderVersion != EncoderVersion || !validID(m.WorkspaceID) || !validID(m.WorkflowID) || !validID(m.Stable.ID) || !validID(m.Candidate.ID) || m.Stable.ID == m.Candidate.ID || !validID(m.Profile.ID) || !validID(m.Dataset.ID) || !validID(m.Environment.ID) || len(m.Dataset.Cases) == 0 {
		return nil, "", fmt.Errorf("%w: manifest identity/schema/cases", ErrInvalid)
	}
	if m.Source != "offline_real" && m.Source != "offline_fixture" {
		return nil, "", fmt.Errorf("%w: evaluation provenance", ErrInvalid)
	}
	for _, model := range []ModelRevision{m.Stable, m.Candidate} {
		if err := validateModelRevision(model); err != nil {
			return nil, "", err
		}
	}
	if m.Dataset.ContentHash == "" || m.Dataset.Version == "" || m.Dataset.BenchmarkID == "" || m.Dataset.ScorerVersion == "" || m.Environment.OS == "" || m.Environment.Arch == "" || m.Environment.CollectorVersion == "" {
		return nil, "", fmt.Errorf("%w: missing reproducibility fields", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, c := range m.Dataset.Cases {
		if !validID(c.ID) || c.Category == "" || seen[c.ID] {
			return nil, "", fmt.Errorf("%w: duplicate/invalid case", ErrInvalid)
		}
		seen[c.ID] = true
	}
	if m.MemoryMode != "not_applicable" && m.MemoryMode != "off" && m.MemoryMode != "snapshot" {
		return nil, "", fmt.Errorf("%w: memory mode", ErrInvalid)
	}
	if err := validateInferenceProfile(m.Profile); err != nil {
		return nil, "", err
	}
	return canonical(m)
}
func immutableBody(v any) ([]byte, string, error) {
	return canonical(struct {
		SchemaVersion  int    `json:"schema_version"`
		EncoderVersion string `json:"encoder_version"`
		Payload        any    `json:"payload"`
	}{1, EncoderVersion, v})
}

// Typed Go values already encode finite numbers in their shortest form. JSON
// has one remaining equivalent representation from float64: negative zero.
func normalizeNumbers(v any) any {
	switch n := v.(type) {
	case json.Number:
		if n.String() == "-0" {
			return json.Number("0")
		}
	case map[string]any:
		for k, value := range n {
			n[k] = normalizeNumbers(value)
		}
	case []any:
		for i, value := range n {
			n[i] = normalizeNumbers(value)
		}
	}
	return v
}

// Ollama accepts both a bare sha256 hexadecimal digest and sha256:<hex>.
func validSHA256(value string) bool {
	return hashPattern.MatchString(strings.TrimPrefix(value, "sha256:"))
}

// validateModelRevision is shared by new registrations/manifests. Historical
// reads keep their original bytes/hash and surface invalid identities as
// ineligible evidence rather than making the audit record unreadable.
func validateModelRevision(model ModelRevision) error {
	if model.SizeBytes != nil && *model.SizeBytes < 0 {
		return fmt.Errorf("%w: negative reported model file size", ErrInvalid)
	}
	if len(model.ParameterSize) > 256 || len(model.Capabilities) > 128 {
		return fmt.Errorf("%w: reported model metadata limit", ErrInvalid)
	}
	for _, capability := range model.Capabilities {
		if strings.TrimSpace(capability) == "" || len(capability) > 256 {
			return fmt.Errorf("%w: invalid reported capability", ErrInvalid)
		}
	}
	if model.FirstDiscoveredAt != "" {
		stamp, err := time.Parse(time.RFC3339Nano, model.FirstDiscoveredAt)
		if err != nil {
			return fmt.Errorf("%w: discovery timestamp", ErrInvalid)
		}
		_, offset := stamp.Zone()
		if offset != 0 {
			return fmt.Errorf("%w: discovery timestamp must be UTC", ErrInvalid)
		}
	}
	if model.MetadataHash != "" && !hashPattern.MatchString(model.MetadataHash) {
		return fmt.Errorf("%w: model metadata hash", ErrInvalid)
	}
	if !validID(model.ID) || !validID(model.GatewayModelID) || strings.TrimSpace(model.Provider) == "" || strings.TrimSpace(model.ModelName) == "" {
		return fmt.Errorf("%w: model identity", ErrInvalid)
	}
	switch model.IdentityLevel {
	case "digest":
		if !validSHA256(model.Digest) {
			return fmt.Errorf("%w: digest identity requires SHA256", ErrInvalid)
		}
	case "declared":
		if strings.TrimSpace(model.DeploymentVersion) == "" || !validSHA256(model.ConfigHash) {
			return fmt.Errorf("%w: declared identity requires deployment version and configuration hash", ErrInvalid)
		}
	case "unknown":
		if strings.TrimSpace(model.UnknownReason) == "" {
			return fmt.Errorf("%w: unknown identity requires reason", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported identity level", ErrInvalid)
	}
	if model.EndpointFingerprint != "" && !hashPattern.MatchString(model.EndpointFingerprint) {
		return fmt.Errorf("%w: endpoint fingerprint must be SHA256, never credentials/URL", ErrInvalid)
	}
	return nil
}
