-- Drop indexes first
DROP INDEX IF EXISTS idx_memories_embedding_hnsw;

-- Drop the memories table
DROP TABLE IF EXISTS memories;

-- Note: We don't drop the vector extension as it might be used by other tables
