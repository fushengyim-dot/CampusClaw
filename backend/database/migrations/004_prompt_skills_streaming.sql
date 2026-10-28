CREATE TABLE IF NOT EXISTS course_agents (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(64) NOT NULL,
    prompt TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_course_agents_class_name (class_id,name),
    UNIQUE KEY uq_course_agents_id_class (id,class_id),
    CONSTRAINT fk_course_agents_class FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS course_skills (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(32) NOT NULL,
    trigger_text VARCHAR(360) NOT NULL,
    subject_text VARCHAR(32) NOT NULL DEFAULT '',
    tools_json JSON NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    description TEXT NOT NULL,
    source_text MEDIUMTEXT NOT NULL,
    seed_key VARCHAR(64) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_course_skills_class_name (class_id,name),
    UNIQUE KEY uq_course_skills_seed (seed_key),
    UNIQUE KEY uq_course_skills_id_class (id,class_id),
    CONSTRAINT fk_course_skills_class FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS course_agent_skills (
    agent_id BIGINT UNSIGNED NOT NULL,
    skill_id BIGINT UNSIGNED NOT NULL,
    class_id BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (agent_id,skill_id),
    KEY ix_course_agent_skills_class (class_id),
    CONSTRAINT fk_course_agent_skills_agent FOREIGN KEY (agent_id,class_id) REFERENCES course_agents(id,class_id) ON DELETE CASCADE,
    CONSTRAINT fk_course_agent_skills_skill FOREIGN KEY (skill_id,class_id) REFERENCES course_skills(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS course_agent_materials (
    agent_id BIGINT UNSIGNED NOT NULL,
    material_id BIGINT UNSIGNED NOT NULL,
    class_id BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (agent_id,material_id),
    KEY ix_course_agent_materials_class (class_id),
    CONSTRAINT fk_course_agent_materials_agent FOREIGN KEY (agent_id,class_id) REFERENCES course_agents(id,class_id) ON DELETE CASCADE,
    CONSTRAINT fk_course_agent_materials_material FOREIGN KEY (material_id,class_id) REFERENCES materials(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS conversations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    class_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    agent_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(96) NOT NULL DEFAULT '新的提问',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    KEY ix_conversations_owner_updated (user_id,class_id,updated_at,id),
    UNIQUE KEY uq_conversations_id_owner (id,user_id,class_id),
    CONSTRAINT fk_conversations_user FOREIGN KEY (user_id,class_id) REFERENCES users(id,class_id),
    CONSTRAINT fk_conversations_agent FOREIGN KEY (agent_id,class_id) REFERENCES course_agents(id,class_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS conversation_messages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    conversation_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    class_id BIGINT UNSIGNED NOT NULL,
    role VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    content MEDIUMTEXT NOT NULL,
    citations_json JSON NULL,
    skill_name VARCHAR(32) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY ix_messages_conversation (conversation_id,id),
    CONSTRAINT ck_course_message_role CHECK (role IN ('user','assistant')),
    CONSTRAINT fk_messages_conversation_owner FOREIGN KEY (conversation_id,user_id,class_id) REFERENCES conversations(id,user_id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
