-- Create artifacts metadata table
CREATE TABLE IF NOT EXISTS artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES sessions(id) ON DELETE SET NULL,
    edge_id STRING,
    tool_call_id STRING,
    type STRING NOT NULL,
    mime_type STRING NOT NULL,
    filename STRING,
    size BIGINT NOT NULL DEFAULT 0,
    reference STRING NOT NULL,
    ttl_seconds INT DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    metadata JSONB,
    INDEX idx_artifacts_session_id (session_id),
    INDEX idx_artifacts_edge_id (edge_id),
    INDEX idx_artifacts_type (type),
    INDEX idx_artifacts_expires_at (expires_at) WHERE expires_at IS NOT NULL,
    INDEX idx_artifacts_created_at (created_at DESC)
);

-- Add comment for documentation
COMMENT ON TABLE artifacts IS 'Stores metadata for artifacts produced by edge tools (screenshots, recordings, files)';
COMMENT ON COLUMN artifacts.reference IS 'Storage reference: file:// for local, s3:// for S3, inline:// for embedded, redacted:// for redacted';
