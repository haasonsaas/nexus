CREATE TABLE IF NOT EXISTS tool_jobs (
    id STRING PRIMARY KEY,
    tool_name STRING NOT NULL,
    tool_call_id STRING NOT NULL,
    status STRING NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    result JSONB,
    error_message STRING
);

CREATE INDEX IF NOT EXISTS tool_jobs_status_idx ON tool_jobs (status);
CREATE INDEX IF NOT EXISTS tool_jobs_created_at_idx ON tool_jobs (created_at DESC);
