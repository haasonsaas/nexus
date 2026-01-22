-- Drop indexes first
DROP INDEX IF EXISTS task_executions_worker_idx;
DROP INDEX IF EXISTS task_executions_stale_idx;
DROP INDEX IF EXISTS task_executions_task_idx;
DROP INDEX IF EXISTS task_executions_running_idx;
DROP INDEX IF EXISTS task_executions_pending_idx;

DROP INDEX IF EXISTS scheduled_tasks_name_idx;
DROP INDEX IF EXISTS scheduled_tasks_agent_idx;
DROP INDEX IF EXISTS scheduled_tasks_due_idx;

-- Drop tables (executions first due to foreign key)
DROP TABLE IF EXISTS task_executions;
DROP TABLE IF EXISTS scheduled_tasks;
