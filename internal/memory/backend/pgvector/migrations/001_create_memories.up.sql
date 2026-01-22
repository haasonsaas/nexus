-- Enable pgvector extension (requires superuser or extension already installed)
CREATE EXTENSION IF NOT EXISTS vector;

-- Create memories table with vector column
CREATE TABLE IF NOT EXISTS memories (
    id UUID PRIMARY KEY,
    session_id TEXT,
    channel_id TEXT,
    agent_id TEXT,
    content TEXT NOT NULL,
    metadata JSONB,
    embedding vector,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memories_session_id ON memories (session_id);
CREATE INDEX IF NOT EXISTS idx_memories_channel_id ON memories (channel_id);
CREATE INDEX IF NOT EXISTS idx_memories_agent_id ON memories (agent_id);
CREATE INDEX IF NOT EXISTS idx_memories_created_at ON memories (created_at);

-- Create HNSW index for fast approximate nearest neighbor search
-- Using cosine distance for normalized embeddings
CREATE INDEX IF NOT EXISTS idx_memories_embedding_hnsw
ON memories USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);
