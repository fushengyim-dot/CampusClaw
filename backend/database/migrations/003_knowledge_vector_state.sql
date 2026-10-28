CREATE TABLE IF NOT EXISTS knowledge_vector_state (
    chunk_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    status ENUM('pending','ready','failed') NOT NULL DEFAULT 'pending',
    content_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    indexed_at DATETIME(6) NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_knowledge_vector_state_chunk FOREIGN KEY (chunk_id) REFERENCES knowledge_chunks(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
