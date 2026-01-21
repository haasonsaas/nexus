-- Create sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id VARCHAR(255) NOT NULL,
    channel VARCHAR(50) NOT NULL,
    channel_id VARCHAR(255) NOT NULL,
    key VARCHAR(767) NOT NULL UNIQUE,
    title TEXT,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    INDEX idx_sessions_agent_id (agent_id),
    INDEX idx_sessions_channel (channel),
    INDEX idx_sessions_key (key)
);

-- Create messages table
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    channel VARCHAR(50) NOT NULL,
    channel_id VARCHAR(255),
    direction VARCHAR(20) NOT NULL,
    role VARCHAR(20) NOT NULL,
    content TEXT NOT NULL,
    attachments JSONB,
    tool_calls JSONB,
    tool_results JSONB,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    INDEX idx_messages_session_id (session_id),
    INDEX idx_messages_created_at (created_at)
);
