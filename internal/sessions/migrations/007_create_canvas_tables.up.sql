-- Create canvas sessions table
CREATE TABLE IF NOT EXISTS canvas_sessions (
    id STRING PRIMARY KEY,
    key STRING NOT NULL,
    workspace_id STRING NOT NULL,
    channel_id STRING NOT NULL,
    thread_ts STRING,
    owner_id STRING,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_canvas_sessions_key ON canvas_sessions (key);
CREATE INDEX IF NOT EXISTS idx_canvas_sessions_workspace ON canvas_sessions (workspace_id);
CREATE INDEX IF NOT EXISTS idx_canvas_sessions_channel ON canvas_sessions (channel_id);
CREATE INDEX IF NOT EXISTS idx_canvas_sessions_thread_ts ON canvas_sessions (thread_ts);

-- Canvas state snapshot
CREATE TABLE IF NOT EXISTS canvas_state (
    session_id STRING PRIMARY KEY REFERENCES canvas_sessions(id) ON DELETE CASCADE,
    state_json JSONB,
    updated_at TIMESTAMPTZ NOT NULL
);

-- Canvas event log
CREATE TABLE IF NOT EXISTS canvas_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id STRING NOT NULL REFERENCES canvas_sessions(id) ON DELETE CASCADE,
    type STRING NOT NULL,
    payload_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_canvas_events_session ON canvas_events (session_id);
CREATE INDEX IF NOT EXISTS idx_canvas_events_session_created ON canvas_events (session_id, created_at DESC);
