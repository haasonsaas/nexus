-- Remove full-text search support

DROP INDEX IF EXISTS idx_memories_content_fts;
ALTER TABLE memories DROP COLUMN IF EXISTS content_tsv;
