CREATE TABLE IF NOT EXISTS account_invitations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    class_id BIGINT UNSIGNED NOT NULL,
    role VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    issued_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6) NULL,
    consumed_at DATETIME(6) NULL,
    UNIQUE KEY uq_account_invitation_hash (token_hash),
    KEY ix_account_invitation_class (class_id, created_at, id),
    CONSTRAINT ck_account_invitation_role CHECK (role IN ('teacher', 'student')),
    CONSTRAINT fk_account_invitation_class FOREIGN KEY (class_id) REFERENCES classes(id),
    CONSTRAINT fk_account_invitation_issuer FOREIGN KEY (issued_by, class_id) REFERENCES users(id, class_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
