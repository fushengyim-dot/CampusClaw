CREATE TABLE IF NOT EXISTS classes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(200) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(191) COLLATE utf8mb4_0900_as_cs NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    class_id BIGINT UNSIGNED NOT NULL,
    UNIQUE KEY uq_users_username (username),
    UNIQUE KEY uq_users_id_class (id, class_id),
    CONSTRAINT ck_users_role CHECK (role IN ('teacher', 'student')),
    CONSTRAINT fk_users_class FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sessions (
    token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    KEY ix_sessions_expiry (expires_at),
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS materials (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    uploaded_by BIGINT UNSIGNED NOT NULL,
    title VARCHAR(200) NOT NULL,
    original_filename VARCHAR(255) NOT NULL,
    storage_key VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    size_bytes BIGINT UNSIGNED NOT NULL,
    sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_url VARCHAR(2048) NULL,
    source_locator VARCHAR(200) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_materials_storage (storage_key),
    UNIQUE KEY uq_materials_id_class (id, class_id),
    KEY ix_materials_class_created (class_id, created_at, id),
    CONSTRAINT fk_materials_class FOREIGN KEY (class_id) REFERENCES classes(id),
    CONSTRAINT fk_materials_uploader FOREIGN KEY (uploaded_by, class_id) REFERENCES users(id, class_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS knowledge_entries (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    material_id BIGINT UNSIGNED NOT NULL,
    class_id BIGINT UNSIGNED NOT NULL,
    content MEDIUMTEXT NOT NULL,
    UNIQUE KEY uq_knowledge_material (material_id),
    CONSTRAINT fk_knowledge_material_class FOREIGN KEY (material_id, class_id) REFERENCES materials(id, class_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS assignments (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(200) NOT NULL,
    CONSTRAINT fk_assignments_class FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS tutor_agents (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(200) NOT NULL,
    CONSTRAINT fk_agents_class FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS skills (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(200) NOT NULL,
    CONSTRAINT fk_skills_class FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
