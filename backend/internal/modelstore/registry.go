package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SaveModelRevision registers immutable identity before an evaluation exists.
// It deliberately shares the same fact table and validation as manifests.
func (s *Store) SaveModelRevision(ctx context.Context, model ModelRevision) error {
	if err := validateModelRevision(model); err != nil {
		return err
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		return insertImmutable(ctx, tx, "model_revisions", model.ID, "identity_hash", model, "gateway_model_id,provider,model_name,identity_level", []any{model.GatewayModelID, model.Provider, model.ModelName, model.IdentityLevel})
	})
}

func readRegistryRecord[T any](ctx context.Context, s *Store, table, hashColumn, id string) (T, error) {
	var value T
	if !validID(id) {
		return value, ErrInvalid
	}
	var body, hash string
	err := s.db.QueryRowContext(ctx, "SELECT body_json,"+hashColumn+" FROM "+table+" WHERE id=?", id).Scan(&body, &hash)
	if err == sql.ErrNoRows {
		return value, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	_, actual, err := canonical(json.RawMessage(body))
	if err != nil || actual != hash {
		return value, fmt.Errorf("%w: registry record hash mismatch", ErrInvalid)
	}
	var envelope struct {
		SchemaVersion  int    `json:"schema_version"`
		EncoderVersion string `json:"encoder_version"`
		Payload        T      `json:"payload"`
	}
	if err = json.Unmarshal([]byte(body), &envelope); err != nil {
		return value, err
	}
	if envelope.SchemaVersion != 1 || envelope.EncoderVersion != EncoderVersion {
		return value, fmt.Errorf("%w: unsupported registry encoding", ErrInvalid)
	}
	return envelope.Payload, nil
}

func (s *Store) GetModelRevision(ctx context.Context, id string) (ModelRevision, error) {
	return readRegistryRecord[ModelRevision](ctx, s, "model_revisions", "identity_hash", id)
}

func (s *Store) SaveInferenceProfile(ctx context.Context, profile InferenceProfile) error {
	if err := validateInferenceProfile(profile); err != nil {
		return err
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		return insertImmutable(ctx, tx, "inference_profiles", profile.ID, "profile_hash", profile, "", nil)
	})
}

func (s *Store) GetInferenceProfile(ctx context.Context, id string) (InferenceProfile, error) {
	return readRegistryRecord[InferenceProfile](ctx, s, "inference_profiles", "profile_hash", id)
}

func validateInferenceProfile(profile InferenceProfile) error {
	if !validID(profile.ID) {
		return fmt.Errorf("%w: profile identity", ErrInvalid)
	}
	for _, o := range []*Options{&profile.Requested, profile.Effective, profile.Reported} {
		if o == nil {
			continue
		}
		if o.KeepAlive != "" && o.KeepAlive != "0" && o.KeepAlive != "-1" {
			if d, err := time.ParseDuration(o.KeepAlive); err != nil || d < 0 {
				return fmt.Errorf("%w: keep_alive duration", ErrInvalid)
			}
		}
		for _, n := range []*int64{o.NumCtx, o.MaxOutput, o.TopK, o.Parallelism, o.DeadlineNS, o.RetryLimit} {
			if n != nil && *n < 0 {
				return fmt.Errorf("%w: negative option", ErrInvalid)
			}
		}
		if o.Temperature != nil && *o.Temperature < 0 {
			return ErrInvalid
		}
		if o.TopP != nil && (*o.TopP < 0 || *o.TopP > 1) {
			return ErrInvalid
		}
	}
	_, _, err := canonical(profile)
	return err
}
