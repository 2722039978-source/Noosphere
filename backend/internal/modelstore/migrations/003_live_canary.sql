CREATE TABLE live_canary_releases (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, body_json TEXT NOT NULL,
  body_hash TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX live_canary_workspace ON live_canary_releases(workspace_id,id);
CREATE TABLE live_canary_requests (
  id TEXT PRIMARY KEY, release_id TEXT NOT NULL REFERENCES live_canary_releases(id),
  workspace_id TEXT NOT NULL, sequence INTEGER NOT NULL, route_epoch INTEGER NOT NULL,
  arm TEXT NOT NULL CHECK(arm IN ('stable','candidate')),
  status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed','interrupted')),
  wall_ns INTEGER NOT NULL DEFAULT 0, body_json TEXT NOT NULL, body_hash TEXT NOT NULL,
  UNIQUE(release_id,sequence)
);
CREATE INDEX live_canary_request_page ON live_canary_requests(workspace_id,release_id,sequence);
CREATE TRIGGER live_request_terminal_immutable BEFORE UPDATE ON live_canary_requests
WHEN OLD.status <> 'running' BEGIN SELECT RAISE(ABORT,'immutable live request'); END;
CREATE TRIGGER live_request_no_delete BEFORE DELETE ON live_canary_requests
BEGIN SELECT RAISE(ABORT,'immutable live request'); END;
CREATE TABLE live_canary_feedback (
  request_id TEXT PRIMARY KEY REFERENCES live_canary_requests(id), release_id TEXT NOT NULL,
  rating TEXT NOT NULL CHECK(rating IN ('good','bad')), body_json TEXT NOT NULL, body_hash TEXT NOT NULL
);
CREATE TRIGGER live_feedback_no_update BEFORE UPDATE ON live_canary_feedback BEGIN SELECT RAISE(ABORT,'immutable live feedback'); END;
CREATE TRIGGER live_feedback_no_delete BEFORE DELETE ON live_canary_feedback BEGIN SELECT RAISE(ABORT,'immutable live feedback'); END;
CREATE TABLE live_canary_events (
  release_id TEXT NOT NULL REFERENCES live_canary_releases(id), sequence INTEGER NOT NULL,
  body_json TEXT NOT NULL, event_hash TEXT NOT NULL, PRIMARY KEY(release_id,sequence)
);
CREATE TRIGGER live_event_no_update BEFORE UPDATE ON live_canary_events BEGIN SELECT RAISE(ABORT,'immutable live event'); END;
CREATE TRIGGER live_event_no_delete BEFORE DELETE ON live_canary_events BEGIN SELECT RAISE(ABORT,'immutable live event'); END;
CREATE TABLE live_canary_receipts (
  workspace_id TEXT NOT NULL, release_id TEXT NOT NULL, operation TEXT NOT NULL,
  key TEXT NOT NULL, request_hash TEXT NOT NULL, response_json TEXT NOT NULL,
  PRIMARY KEY(workspace_id,release_id,operation,key)
);
