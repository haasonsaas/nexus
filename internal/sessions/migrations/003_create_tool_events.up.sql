-- Tool calls: one row per tool call emitted by the model
CREATE TABLE IF NOT EXISTS tool_calls (
    id           STRING PRIMARY KEY,         -- tool call id from provider
    session_id   STRING NOT NULL,
    message_id   STRING NULL,                -- inbound user msg that started this run
    tool_name    STRING NOT NULL,
    input_json   JSONB  NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS tool_calls_session_time_idx
    ON tool_calls (session_id, created_at);

CREATE INDEX IF NOT EXISTS tool_calls_tool_name_idx
    ON tool_calls (tool_name);

-- Tool results: one row per tool result
CREATE TABLE IF NOT EXISTS tool_results (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    STRING NOT NULL,
    message_id    STRING NULL,
    tool_call_id  STRING NOT NULL,
    is_error      BOOL NOT NULL DEFAULT false,
    content       STRING NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS tool_results_session_time_idx
    ON tool_results (session_id, created_at);

CREATE INDEX IF NOT EXISTS tool_results_call_id_idx
    ON tool_results (tool_call_id);
