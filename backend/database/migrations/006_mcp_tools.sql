CREATE TABLE IF NOT EXISTS mcp_servers (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(64) NOT NULL,
    endpoint VARCHAR(512) NOT NULL,
    token_ciphertext VARBINARY(2048) NULL,
    connected TINYINT(1) NOT NULL DEFAULT 0,
    last_error_code VARCHAR(64) NULL,
    discovered_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_mcp_servers_class_name (class_id,name),
    UNIQUE KEY uq_mcp_servers_id_class (id,class_id),
    CONSTRAINT fk_mcp_servers_class FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS mcp_server_tools (
    server_id BIGINT UNSIGNED NOT NULL,
    class_id BIGINT UNSIGNED NOT NULL,
    tool_name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL,
    input_schema JSON NOT NULL,
    schema_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (server_id,tool_name),
    KEY ix_mcp_server_tools_class (class_id),
    CONSTRAINT fk_mcp_server_tools_server FOREIGN KEY (server_id,class_id) REFERENCES mcp_servers(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS course_skill_tool_bindings (
    skill_id BIGINT UNSIGNED NOT NULL,
    server_id BIGINT UNSIGNED NOT NULL,
    class_id BIGINT UNSIGNED NOT NULL,
    tool_name VARCHAR(128) NOT NULL,
    confirmed_schema_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (skill_id,server_id,tool_name),
    KEY ix_skill_tool_bindings_server (server_id,tool_name,class_id),
    CONSTRAINT fk_skill_tool_bindings_skill FOREIGN KEY (skill_id,class_id) REFERENCES course_skills(id,class_id) ON DELETE CASCADE,
    CONSTRAINT fk_skill_tool_bindings_server FOREIGN KEY (server_id,class_id) REFERENCES mcp_servers(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS mcp_tool_calls (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    conversation_id BIGINT UNSIGNED NOT NULL,
    turn_number INT UNSIGNED NOT NULL,
    server_id BIGINT UNSIGNED NULL,
    server_name_snapshot VARCHAR(64) NOT NULL,
    tool_name VARCHAR(128) NOT NULL,
    schema_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    status VARCHAR(24) NOT NULL,
    safe_summary VARCHAR(500) NOT NULL,
    occurred_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY ix_mcp_tool_calls_user (class_id,user_id,occurred_at),
    KEY ix_mcp_tool_calls_conversation (class_id,conversation_id,turn_number),
    CONSTRAINT fk_mcp_tool_calls_class FOREIGN KEY (class_id) REFERENCES classes(id),
    CONSTRAINT fk_mcp_tool_calls_user FOREIGN KEY (user_id,class_id) REFERENCES users(id,class_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
