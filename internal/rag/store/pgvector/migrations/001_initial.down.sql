-- Drop indexes first
DROP INDEX IF EXISTS idx_rag_document_chunks_embedding_hnsw;
DROP INDEX IF EXISTS idx_rag_document_chunks_metadata_tags;
DROP INDEX IF EXISTS idx_rag_document_chunks_metadata_channel;
DROP INDEX IF EXISTS idx_rag_document_chunks_metadata_session;
DROP INDEX IF EXISTS idx_rag_document_chunks_metadata_agent;
DROP INDEX IF EXISTS idx_rag_document_chunks_document_id;

DROP INDEX IF EXISTS idx_rag_documents_metadata_tags;
DROP INDEX IF EXISTS idx_rag_documents_metadata_channel;
DROP INDEX IF EXISTS idx_rag_documents_metadata_session;
DROP INDEX IF EXISTS idx_rag_documents_metadata_agent;
DROP INDEX IF EXISTS idx_rag_documents_updated_at;
DROP INDEX IF EXISTS idx_rag_documents_created_at;
DROP INDEX IF EXISTS idx_rag_documents_source;

-- Drop tables
DROP TABLE IF EXISTS rag_document_chunks;
DROP TABLE IF EXISTS rag_documents;
