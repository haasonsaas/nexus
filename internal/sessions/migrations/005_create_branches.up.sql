-- Create branches table for conversation branching
CREATE TABLE IF NOT EXISTS branches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_branch_id UUID REFERENCES branches(id) ON DELETE SET NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    branch_point BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    is_primary BOOLEAN NOT NULL DEFAULT false,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    merged_at TIMESTAMP WITH TIME ZONE,

    INDEX idx_branches_session_id (session_id),
    INDEX idx_branches_parent_branch_id (parent_branch_id),
    INDEX idx_branches_status (status),
    INDEX idx_branches_session_primary (session_id, is_primary) WHERE is_primary = true
);

-- Create branch_merges table to track merge history
CREATE TABLE IF NOT EXISTS branch_merges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_branch_id UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    target_branch_id UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    strategy VARCHAR(50) NOT NULL,
    source_sequence_start BIGINT NOT NULL,
    source_sequence_end BIGINT NOT NULL,
    target_sequence_insert BIGINT NOT NULL,
    message_count INT NOT NULL DEFAULT 0,
    metadata JSONB,
    merged_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    merged_by VARCHAR(255),

    INDEX idx_branch_merges_source (source_branch_id),
    INDEX idx_branch_merges_target (target_branch_id),
    INDEX idx_branch_merges_merged_at (merged_at DESC)
);

-- Add branch_id and sequence_num columns to messages table
ALTER TABLE messages ADD COLUMN IF NOT EXISTS branch_id UUID REFERENCES branches(id) ON DELETE CASCADE;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS sequence_num BIGINT NOT NULL DEFAULT 0;

-- Create index for branch-aware message queries
CREATE INDEX IF NOT EXISTS idx_messages_branch_id ON messages (branch_id);
CREATE INDEX IF NOT EXISTS idx_messages_branch_sequence ON messages (branch_id, sequence_num);

-- Create a partial unique index to ensure only one primary branch per session
CREATE UNIQUE INDEX IF NOT EXISTS idx_branches_unique_primary
ON branches (session_id) WHERE is_primary = true;

-- Function to auto-create primary branch for new sessions
CREATE OR REPLACE FUNCTION create_primary_branch_for_session()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO branches (session_id, name, description, is_primary, status)
    VALUES (NEW.id, 'main', 'Primary conversation branch', true, 'active');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to auto-create primary branch (disabled by default for backward compat)
-- Uncomment to enable:
-- CREATE TRIGGER trigger_create_primary_branch
--     AFTER INSERT ON sessions
--     FOR EACH ROW
--     EXECUTE FUNCTION create_primary_branch_for_session();

-- Migration helper: Create primary branches for existing sessions
-- This is safe to run multiple times due to the unique index
INSERT INTO branches (session_id, name, description, is_primary, status, created_at, updated_at)
SELECT s.id, 'main', 'Primary conversation branch', true, 'active', s.created_at, s.updated_at
FROM sessions s
WHERE NOT EXISTS (
    SELECT 1 FROM branches b WHERE b.session_id = s.id AND b.is_primary = true
)
ON CONFLICT DO NOTHING;

-- Migration helper: Assign existing messages to primary branches
UPDATE messages m
SET branch_id = (
    SELECT b.id FROM branches b
    WHERE b.session_id = m.session_id AND b.is_primary = true
    LIMIT 1
),
sequence_num = (
    SELECT COUNT(*) FROM messages m2
    WHERE m2.session_id = m.session_id
    AND m2.created_at <= m.created_at
    AND (m2.branch_id IS NULL OR m2.branch_id = m.branch_id)
)
WHERE m.branch_id IS NULL;
