package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Keep applied migrations immutable. Future changes require a new version.
//
//go:embed migrations/001_initial.sql
var initialSchema string

//go:embed migrations/002_knowledge_chunks.sql
var knowledgeChunksSchema string

//go:embed migrations/003_knowledge_vector_state.sql
var knowledgeVectorStateSchema string

//go:embed migrations/004_prompt_skills_streaming.sql
var promptSkillsStreamingSchema string

//go:embed migrations/005_builtin_applications.sql
var builtinApplicationsSchema string

//go:embed migrations/006_mcp_tools.sql
var mcpToolsSchema string

//go:embed migrations/007_mcp_server_presets.sql
var mcpServerPresetsSchema string

//go:embed migrations/008_account_invitations.sql
var accountInvitationsSchema string

var baseSchemaTables = []string{"classes", "users", "sessions", "materials", "knowledge_entries", "assignments", "tutor_agents", "skills"}
var schemaTables = append(append([]string{}, baseSchemaTables...), "knowledge_chunks", "knowledge_vector_state", "course_agents", "course_skills", "course_agent_skills", "course_agent_materials", "conversations", "conversation_messages", "course_builtin_agents", "mcp_servers", "mcp_server_tools", "course_skill_tool_bindings", "mcp_tool_calls", "mcp_server_presets", "account_invitations")
var ErrSchema = errors.New("数据表初始化未通过；请检查数据库可用性和结构版本，不要删除已有数据后重试")
var ErrSchemaConflict = errors.New("数据表版本冲突或已完成结构缺失；已停止，不覆盖已有数据")

type schemaMigration struct {
	version int
	sql     string
	tables  []string
}

func schemaMigrations() []schemaMigration {
	return []schemaMigration{
		{version: 1, sql: initialSchema, tables: baseSchemaTables},
		{version: 2, sql: knowledgeChunksSchema, tables: []string{"knowledge_chunks"}},
		{version: 3, sql: knowledgeVectorStateSchema, tables: []string{"knowledge_vector_state"}},
		{version: 4, sql: promptSkillsStreamingSchema, tables: []string{"course_agents", "course_skills", "course_agent_skills", "course_agent_materials", "conversations", "conversation_messages"}},
		{version: 5, sql: builtinApplicationsSchema, tables: []string{"course_builtin_agents"}},
		{version: 6, sql: mcpToolsSchema, tables: []string{"mcp_servers", "mcp_server_tools", "course_skill_tool_bindings", "mcp_tool_calls"}},
		{version: 7, sql: mcpServerPresetsSchema, tables: []string{"mcp_server_presets"}},
		{version: 8, sql: accountInvitationsSchema, tables: []string{"account_invitations"}},
	}
}

// InitSchema only prepares structure. It never creates accounts or resets data.
// MySQL DDL commits individually; an unfinished matching migration can resume.
func InitSchema(parent context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return ErrSchema
	}
	defer conn.Close()
	var dbName string
	if conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&dbName) != nil || dbName == "" {
		return ErrSchema
	}
	lock := fmt.Sprintf("campusclaw-schema-%x", sha256.Sum256([]byte(dbName)))[:63]
	var acquired int
	if conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 1)", lock).Scan(&acquired) != nil || acquired != 1 {
		return ErrSchema
	}
	defer func() {
		releaseCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		var released int
		if conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", lock).Scan(&released) != nil || released != 1 {
			// Discard the physical connection rather than returning a held lock to the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INT NOT NULL PRIMARY KEY,
        checksum CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
        completed_at DATETIME(6) NULL
    ) ENGINE=InnoDB`); err != nil {
		return ErrSchema
	}
	migrations := schemaMigrations()
	var unsupported int
	if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version < 1 OR version > ?", len(migrations)).Scan(&unsupported) != nil {
		return ErrSchema
	}
	if unsupported != 0 {
		return ErrSchemaConflict
	}
	for _, migration := range migrations {
		checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(migration.sql)))
		var saved string
		var completed sql.NullTime
		err = conn.QueryRowContext(ctx, "SELECT checksum, completed_at FROM schema_migrations WHERE version=?", migration.version).Scan(&saved, &completed)
		if errors.Is(err, sql.ErrNoRows) {
			for _, table := range migration.tables {
				exists, e := schemaTableExists(ctx, conn, table)
				if e != nil {
					return ErrSchema
				}
				if exists {
					return ErrSchemaConflict
				}
			}
			if _, err = conn.ExecContext(ctx, "INSERT INTO schema_migrations(version, checksum) VALUES (?, ?)", migration.version, checksum); err != nil {
				return ErrSchema
			}
		} else if err != nil {
			return ErrSchema
		} else if saved != checksum {
			return ErrSchemaConflict
		}
		if !completed.Valid {
			// Migrations contain idempotent CREATE TABLE statements only, so an
			// interrupted DDL migration can resume safely using its stored checksum.
			for _, statement := range strings.Split(migration.sql, ";") {
				if strings.TrimSpace(statement) == "" {
					continue
				}
				if _, err = conn.ExecContext(ctx, statement); err != nil {
					return ErrSchema
				}
			}
		}
		for _, table := range migration.tables {
			exists, e := schemaTableExists(ctx, conn, table)
			if e != nil {
				return ErrSchema
			}
			if !exists {
				return ErrSchemaConflict
			}
		}
		if !completed.Valid {
			if _, err = conn.ExecContext(ctx, "UPDATE schema_migrations SET completed_at=CURRENT_TIMESTAMP(6) WHERE version=? AND completed_at IS NULL", migration.version); err != nil {
				return ErrSchema
			}
		}
	}
	for _, table := range schemaTables {
		exists, e := schemaTableExists(ctx, conn, table)
		if e != nil {
			return ErrSchema
		}
		if !exists {
			return ErrSchemaConflict
		}
	}
	return nil
}

func schemaTableExists(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	var n int
	err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=? AND table_type='BASE TABLE'", table).Scan(&n)
	return n == 1, err
}
