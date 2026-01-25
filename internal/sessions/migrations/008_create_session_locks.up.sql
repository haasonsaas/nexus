CREATE TABLE IF NOT EXISTS session_locks (
  session_id STRING PRIMARY KEY,
  owner_id STRING NOT NULL,
  acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS session_locks_expires_at_idx
  ON session_locks (expires_at);
