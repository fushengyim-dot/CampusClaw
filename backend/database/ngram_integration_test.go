package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestNgramParserIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_MYSQL_INTEGRATION") != "1" {
		t.Skip("explicit MySQL integration environment required")
		return
	}
	db, ctx, _ := integrationDB(t)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal("MySQL integration connection failed")
	}
	defer conn.Close()
	var tokenSize int
	if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.ngram_token_size").Scan(&tokenSize); err != nil || tokenSize != 2 {
		t.Fatalf("MySQL ngram token size must be 2; observed %d", tokenSize)
	}
	probe := fmt.Sprintf("campusclaw_ngram_probe_%x", time.Now().UnixNano())
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, `DROP TABLE IF EXISTS `+probe); err != nil {
			t.Errorf("could not remove test-owned MySQL probe table: %v", err)
			return
		}
		t.Log("CHECK: test-owned probe table removed using an independent connection")
	}()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE `+probe+` (
		id INT NOT NULL PRIMARY KEY,
		content TEXT NOT NULL,
		FULLTEXT KEY ft_probe_content (content) WITH PARSER ngram
	) ENGINE=InnoDB`); err != nil {
		t.Fatalf("MySQL ngram parser probe table could not be created: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO "+probe+"(id,content) VALUES(1,?)", "授权材料包含中文检索片段"); err != nil {
		t.Fatal("temporary ngram probe insert failed")
	}
	var found int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+probe+" WHERE MATCH(content) AGAINST (? IN BOOLEAN MODE)", "+授权").Scan(&found); err != nil || found != 1 {
		t.Fatalf("MySQL ngram full-text search did not find the Chinese bigram: matches=%d err=%v", found, err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal("probe connection could not be closed before independent cleanup")
	}
	t.Log("CHECK: MySQL ngram parser with token size 2 matched a Chinese bigram")
}
