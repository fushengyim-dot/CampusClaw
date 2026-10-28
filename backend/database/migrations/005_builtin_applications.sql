CREATE TABLE IF NOT EXISTS course_builtin_agents (
    class_id BIGINT UNSIGNED NOT NULL,
    builtin_key VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    agent_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (class_id,builtin_key),
    UNIQUE KEY uq_course_builtin_agent (agent_id,class_id),
    CONSTRAINT fk_course_builtin_class FOREIGN KEY (class_id) REFERENCES classes(id),
    CONSTRAINT fk_course_builtin_agent FOREIGN KEY (agent_id,class_id) REFERENCES course_agents(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
