-- Remove branch_id and sequence_num from messages
DROP INDEX IF EXISTS idx_messages_branch_sequence;
DROP INDEX IF EXISTS idx_messages_branch_id;
ALTER TABLE messages DROP COLUMN IF EXISTS sequence_num;
ALTER TABLE messages DROP COLUMN IF EXISTS branch_id;

-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_create_primary_branch ON sessions;
DROP FUNCTION IF EXISTS create_primary_branch_for_session();

-- Drop unique primary index
DROP INDEX IF EXISTS idx_branches_unique_primary;

-- Drop branch_merges table
DROP TABLE IF EXISTS branch_merges;

-- Drop branches table
DROP TABLE IF EXISTS branches;
