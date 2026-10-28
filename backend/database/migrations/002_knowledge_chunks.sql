CREATE TABLE IF NOT EXISTS knowledge_chunks (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    knowledge_entry_id BIGINT UNSIGNED NOT NULL,
    chunk_index INT UNSIGNED NOT NULL,
    chunk_text MEDIUMTEXT NOT NULL,
    char_start INT UNSIGNED NOT NULL,
    char_end INT UNSIGNED NOT NULL,
    index_status ENUM('pending','ready','failed') NOT NULL DEFAULT 'pending',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_knowledge_chunks_entry_index (knowledge_entry_id, chunk_index),
    KEY ix_knowledge_chunks_entry_status (knowledge_entry_id, index_status),
    FULLTEXT KEY ft_knowledge_chunks_text (chunk_text) WITH PARSER ngram,
    CONSTRAINT fk_knowledge_chunks_entry FOREIGN KEY (knowledge_entry_id) REFERENCES knowledge_entries(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
