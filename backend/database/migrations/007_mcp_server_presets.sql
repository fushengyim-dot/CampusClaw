CREATE TABLE IF NOT EXISTS mcp_server_presets (
    server_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    preset_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_mcp_server_presets_class_key (class_id,preset_key),
    CONSTRAINT fk_mcp_server_presets_server FOREIGN KEY (server_id,class_id) REFERENCES mcp_servers(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
