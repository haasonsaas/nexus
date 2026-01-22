-- Create scheduled_tasks table for cron-based agent triggers
CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id STRING PRIMARY KEY,
    name STRING NOT NULL,
    description STRING,
    agent_id STRING NOT NULL,
    schedule STRING NOT NULL,
    timezone STRING,
    prompt STRING NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    status STRING NOT NULL DEFAULT 'active',
    next_run_at TIMESTAMPTZ NOT NULL,
    last_run_at TIMESTAMPTZ,
    last_execution_id STRING,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB DEFAULT '{}'
);

-- Index for finding due tasks efficiently
CREATE INDEX IF NOT EXISTS scheduled_tasks_due_idx
    ON scheduled_tasks (status, next_run_at)
    WHERE status = 'active';

-- Index for listing tasks by agent
CREATE INDEX IF NOT EXISTS scheduled_tasks_agent_idx
    ON scheduled_tasks (agent_id, status);

-- Index for looking up tasks by name
CREATE INDEX IF NOT EXISTS scheduled_tasks_name_idx
    ON scheduled_tasks (name);

-- Create task_executions table for execution history
CREATE TABLE IF NOT EXISTS task_executions (
    id STRING PRIMARY KEY,
    task_id STRING NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
    status STRING NOT NULL DEFAULT 'pending',
    scheduled_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    session_id STRING,
    prompt STRING NOT NULL,
    response STRING,
    error STRING,
    attempt_number INT NOT NULL DEFAULT 1,
    worker_id STRING,
    locked_at TIMESTAMPTZ,
    locked_until TIMESTAMPTZ,
    duration BIGINT DEFAULT 0,
    metadata JSONB DEFAULT '{}'
);

-- Index for acquiring pending executions with distributed locking
-- This index supports SELECT FOR UPDATE SKIP LOCKED queries
CREATE INDEX IF NOT EXISTS task_executions_pending_idx
    ON task_executions (status, scheduled_at, locked_until)
    WHERE status = 'pending';

-- Index for finding running executions (overlap check)
CREATE INDEX IF NOT EXISTS task_executions_running_idx
    ON task_executions (task_id, status)
    WHERE status = 'running';

-- Index for listing executions by task
CREATE INDEX IF NOT EXISTS task_executions_task_idx
    ON task_executions (task_id, scheduled_at DESC);

-- Index for cleanup of stale executions
CREATE INDEX IF NOT EXISTS task_executions_stale_idx
    ON task_executions (status, started_at)
    WHERE status = 'running';

-- Index for worker-specific queries
CREATE INDEX IF NOT EXISTS task_executions_worker_idx
    ON task_executions (worker_id, status);
