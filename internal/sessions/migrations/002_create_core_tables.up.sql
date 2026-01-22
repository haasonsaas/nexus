-- Create users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email STRING,
    name STRING,
    avatar_url STRING,
    provider STRING,
    provider_id STRING,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (lower(email)) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_provider ON users (provider, provider_id) WHERE provider IS NOT NULL AND provider_id IS NOT NULL;

-- Create agents table
CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    name STRING NOT NULL,
    system_prompt STRING,
    model STRING,
    provider STRING,
    tools STRING[],
    config JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    INDEX idx_agents_user_id (user_id)
);

-- Create channel connections table
CREATE TABLE IF NOT EXISTS channel_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    channel_type STRING NOT NULL,
    channel_id STRING NOT NULL,
    status STRING NOT NULL,
    config JSONB,
    connected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_activity_at TIMESTAMPTZ,
    INDEX idx_channel_connections_user_id (user_id),
    INDEX idx_channel_connections_type (channel_type),
    INDEX idx_channel_connections_status (status)
);
