-- Enable pgvector extension (requires superuser or extension already installed)
CREATE EXTENSION IF NOT EXISTS vector;

-- Create memories table with vector column
CREATE TABLE IF NOT EXISTS memories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id VARCHAR(255),
    channel_id VARCHAR(255),
    agent_id VARCHAR(255),
    content TEXT NOT NULL,
    metadata JSONB,
    embedding vector,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    INDEX idx_memories_session_id (session_id),
    INDEX idx_memories_channel_id (channel_id),
    INDEX idx_memories_agent_id (agent_id),
    INDEX idx_memories_created_at (created_at)
);

-- Create HNSW index for fast approximate nearest neighbor search
-- Using cosine distance for normalized embeddings
CREATE INDEX IF NOT EXISTS idx_memories_embedding_hnsw
ON memories USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);
