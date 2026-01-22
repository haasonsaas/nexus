-- Enable pgvector extension if not already enabled
CREATE EXTENSION IF NOT EXISTS vector;

-- Documents table stores document metadata
CREATE TABLE IF NOT EXISTS rag_documents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source TEXT NOT NULL,
    source_uri TEXT,
    content_type TEXT,
    content TEXT NOT NULL,
    metadata JSONB DEFAULT '{}'::jsonb,
    chunk_count INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Document chunks table stores chunked content with embeddings
CREATE TABLE IF NOT EXISTS rag_document_chunks (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES rag_documents(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    content TEXT NOT NULL,
    start_offset INTEGER NOT NULL,
    end_offset INTEGER NOT NULL,
    metadata JSONB DEFAULT '{}'::jsonb,
    token_count INTEGER DEFAULT 0,
    embedding vector,  -- Dimension set dynamically or defaults to 1536
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_rag_documents_source ON rag_documents(source);
CREATE INDEX IF NOT EXISTS idx_rag_documents_created_at ON rag_documents(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_rag_documents_updated_at ON rag_documents(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_rag_documents_metadata_agent ON rag_documents USING GIN ((metadata->'agent_id'));
CREATE INDEX IF NOT EXISTS idx_rag_documents_metadata_session ON rag_documents USING GIN ((metadata->'session_id'));
CREATE INDEX IF NOT EXISTS idx_rag_documents_metadata_channel ON rag_documents USING GIN ((metadata->'channel_id'));
CREATE INDEX IF NOT EXISTS idx_rag_documents_metadata_tags ON rag_documents USING GIN ((metadata->'tags'));

CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_document_id ON rag_document_chunks(document_id);
CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_metadata_agent ON rag_document_chunks USING GIN ((metadata->'agent_id'));
CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_metadata_session ON rag_document_chunks USING GIN ((metadata->'session_id'));
CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_metadata_channel ON rag_document_chunks USING GIN ((metadata->'channel_id'));
CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_metadata_tags ON rag_document_chunks USING GIN ((metadata->'tags'));

-- Vector similarity search index (using IVFFlat for better performance on large datasets)
-- Note: This needs to be created after data is inserted for optimal performance
-- CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_embedding ON rag_document_chunks
--     USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- For smaller datasets, use HNSW index
CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_embedding_hnsw ON rag_document_chunks
    USING hnsw (embedding vector_cosine_ops);
