CREATE TABLE dataset_definitions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    benchmark_id TEXT NOT NULL,
    version TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    document_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(workspace_id, benchmark_id, version)
);
CREATE INDEX dataset_definitions_workspace ON dataset_definitions(workspace_id, created_at, id);
CREATE TABLE dataset_catalogue_state (
    dataset_id TEXT PRIMARY KEY REFERENCES dataset_definitions(id),
    archived INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0,1))
);
CREATE TRIGGER dataset_definitions_no_UPDATE BEFORE UPDATE ON dataset_definitions BEGIN SELECT RAISE(ABORT,'immutable record'); END;
CREATE TRIGGER dataset_definitions_no_DELETE BEFORE DELETE ON dataset_definitions BEGIN SELECT RAISE(ABORT,'immutable record'); END;
