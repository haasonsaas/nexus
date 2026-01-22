-- Add full-text search column for BM25/hybrid search support
-- Using a generated column for automatic tsvector maintenance

ALTER TABLE memories ADD COLUMN IF NOT EXISTS content_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('english', coalesce(content, ''))) STORED;

-- Create GIN index for efficient full-text search
CREATE INDEX IF NOT EXISTS idx_memories_content_fts ON memories USING GIN (content_tsv);
