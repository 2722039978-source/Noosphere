package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// DatasetDefinition is the immutable, workspace-owned source specification.
// Catalogue visibility is mutable; source bytes and revisions never are.
type DatasetDefinition struct {
	ID          string
	WorkspaceID string
	BenchmarkID string
	Version     string
	ContentHash string
	Document    json.RawMessage
	CreatedAt   string
	Archived    bool
}

func (s *Store) SaveDatasetDefinition(ctx context.Context, d DatasetDefinition) (DatasetDefinition, error) {
	if !validID(d.ID) || !validID(d.WorkspaceID) || !validID(d.BenchmarkID) || !validID(d.Version) || !hashPattern.MatchString(d.ContentHash) || len(d.Document) == 0 || len(d.Document) > 64<<10 || digest(d.Document) != d.ContentHash || !json.Valid(d.Document) {
		return DatasetDefinition{}, ErrInvalid
	}
	err := s.write(ctx, func(tx *sql.Tx) error {
		var id, hash, body string
		err := tx.QueryRowContext(ctx, "SELECT id,content_hash,document_json FROM dataset_definitions WHERE workspace_id=? AND benchmark_id=? AND version=?", d.WorkspaceID, d.BenchmarkID, d.Version).Scan(&id, &hash, &body)
		if err == nil {
			if id != d.ID || hash != d.ContentHash || body != string(d.Document) {
				return ErrRevisionConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO dataset_definitions(id,workspace_id,benchmark_id,version,content_hash,document_json,created_at) VALUES(?,?,?,?,?,?,?)", d.ID, d.WorkspaceID, d.BenchmarkID, d.Version, d.ContentHash, string(d.Document), now())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO dataset_catalogue_state(dataset_id,archived) VALUES(?,0)", d.ID)
		return err
	})
	if err != nil {
		return DatasetDefinition{}, err
	}
	return s.GetDatasetDefinition(ctx, d.WorkspaceID, d.ID)
}

func scanDataset(row interface{ Scan(...any) error }) (DatasetDefinition, error) {
	var d DatasetDefinition
	var raw string
	err := row.Scan(&d.ID, &d.WorkspaceID, &d.BenchmarkID, &d.Version, &d.ContentHash, &raw, &d.CreatedAt, &d.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.Document = json.RawMessage(raw)
	if digest(d.Document) != d.ContentHash {
		return DatasetDefinition{}, fmt.Errorf("%w: dataset definition hash mismatch", ErrInsufficientEvidence)
	}
	return d, nil
}

const selectDatasetDefinition = "SELECT d.id,d.workspace_id,d.benchmark_id,d.version,d.content_hash,d.document_json,d.created_at,s.archived FROM dataset_definitions d JOIN dataset_catalogue_state s ON s.dataset_id=d.id"

func (s *Store) GetDatasetDefinition(ctx context.Context, workspaceID, id string) (DatasetDefinition, error) {
	if !validID(workspaceID) || !validID(id) {
		return DatasetDefinition{}, ErrInvalid
	}
	return scanDataset(s.db.QueryRowContext(ctx, selectDatasetDefinition+" WHERE d.workspace_id=? AND d.id=?", workspaceID, id))
}

func (s *Store) ListDatasetDefinitions(ctx context.Context, workspaceID string, includeArchived bool) ([]DatasetDefinition, error) {
	if !validID(workspaceID) {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, selectDatasetDefinition+" WHERE d.workspace_id=? AND (s.archived=0 OR ?) ORDER BY d.created_at DESC,d.id", workspaceID, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DatasetDefinition{}
	for rows.Next() {
		d, err := scanDataset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ArchiveDatasetDefinition(ctx context.Context, workspaceID, id string, archived bool) (DatasetDefinition, error) {
	if !validID(workspaceID) || !validID(id) {
		return DatasetDefinition{}, ErrInvalid
	}
	err := s.write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "UPDATE dataset_catalogue_state SET archived=? WHERE dataset_id IN (SELECT id FROM dataset_definitions WHERE id=? AND workspace_id=?)", archived, id, workspaceID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return DatasetDefinition{}, err
	}
	return s.GetDatasetDefinition(ctx, workspaceID, id)
}
