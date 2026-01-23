-- Create artifacts metadata table
CREATE TABLE IF NOT EXISTS artifacts (
    id STRING PRIMARY KEY,
    session_id STRING,
    edge_id STRING,
    type STRING,
    mime_type STRING,
    filename STRING,
    size INT8,
    reference STRING,
    ttl_seconds INT4,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_artifacts_session_id ON artifacts (session_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_edge_id ON artifacts (edge_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_type ON artifacts (type);
CREATE INDEX IF NOT EXISTS idx_artifacts_created_at ON artifacts (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_artifacts_expires_at ON artifacts (expires_at);
