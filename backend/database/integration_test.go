package database

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/config"
	"campusclaw/backend/handlers"
	"github.com/go-sql-driver/mysql"
)

func integrationConfig(t *testing.T) config.Config {
	t.Helper()
	if os.Getenv("CAMPUSCLAW_MYSQL_INTEGRATION") != "1" {
		t.Skip("explicit isolated MySQL integration environment required")
	}
	c, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal("integration configuration invalid")
	}
	return c
}

func integrationDB(t *testing.T) (*sql.DB, context.Context, config.Config) {
	t.Helper()
	c := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	db, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, ctx, c
}

func TestIntegrationConnectionAndScope(t *testing.T) {
	db, ctx, c := integrationDB(t)
	var version, dbName, user string
	if db.QueryRowContext(ctx, "SELECT VERSION(), DATABASE(), CURRENT_USER()").Scan(&version, &dbName, &user) != nil {
		t.Fatal("server identity check failed")
	}
	if dbName != c.MySQLDatabase || user != "campusclaw@%" {
		t.Fatal("unexpected database or application account")
	}
	t.Logf("MySQL version: %s; dedicated application account verified", version)
	_, err := db.ExecContext(ctx, "SELECT 1 FROM mysql.user LIMIT 1")
	var denied *mysql.MySQLError
	if !errors.As(err, &denied) || denied.Number != 1142 {
		t.Fatal("application account unexpectedly has mysql.user access or unexpected failure")
	}
	w := httptest.NewRecorder()
	handlers.New().ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 503 {
		t.Fatal("database connectivity must not imply application readiness")
	}
}

func TestIntegrationWrongPassword(t *testing.T) {
	c := integrationConfig(t)
	c.MySQLPassword = "intentionally-incorrect-test-password"
	db, err := Open(context.Background(), c)
	if db != nil {
		_ = db.Close()
	}
	if err != ErrUnavailable {
		t.Fatal("wrong password not safely rejected")
	}
}

func TestIntegrationWrongDatabase(t *testing.T) {
	c := integrationConfig(t)
	c.MySQLDatabase = "campusclaw_deliberately_absent"
	db, err := Open(context.Background(), c)
	if db != nil {
		_ = db.Close()
	}
	if err != ErrUnavailable {
		t.Fatal("wrong database not safely rejected")
	}
}

func TestIntegrationUnreachablePort(t *testing.T) {
	c := integrationConfig(t)
	c.MySQLPort = 1
	db, err := Open(context.Background(), c)
	if db != nil {
		_ = db.Close()
	}
	if err != ErrUnavailable {
		t.Fatal("unreachable database not safely rejected")
	}
}

func TestIntegrationPersistence(t *testing.T) {
	db, ctx, _ := integrationDB(t)
	if os.Getenv("CAMPUSCLAW_PERSISTENCE_MODE") != "read" {
		if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS connection_check_probe (id INT PRIMARY KEY, marker VARCHAR(64) NOT NULL)"); err != nil {
			t.Fatal("probe table creation failed")
		}
		var b [32]byte
		if _, err := rand.Read(b[:]); err != nil {
			t.Fatal("probe marker generation failed")
		}
		if _, err := db.ExecContext(ctx, "INSERT IGNORE INTO connection_check_probe (id,marker) VALUES (1,?)", hex.EncodeToString(b[:])); err != nil {
			t.Fatal("probe write failed")
		}
	}
	var marker string
	if db.QueryRowContext(ctx, "SELECT marker FROM connection_check_probe WHERE id=1").Scan(&marker) != nil {
		t.Fatal("persisted probe record missing")
	}
	if len(marker) != 64 {
		t.Fatal("unexpected probe record")
	}
	hash := sha256.Sum256([]byte(marker))
	t.Logf("PROBE_SHA256=%x", hash)
}

func TestIntegrationRealServerRemainsUnready(t *testing.T) {
	c := integrationConfig(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/check/bin/campusclaw-linux")
	cmd.Env = append(os.Environ(), fmt.Sprintf("API_PORT=%d", port))
	var captured bytes.Buffer
	cmd.Stdout = &captured
	cmd.Stderr = &captured
	if cmd.Start() != nil {
		t.Fatal("server did not start")
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	for i := 0; i < 40; i++ {
		response, err = client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("real server did not respond")
	}
	_ = response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("connected database incorrectly made platform ready")
	}
	if cmd.Process.Signal(os.Interrupt) != nil || cmd.Wait() != nil {
		t.Fatal("server did not shut down gracefully")
	}
	for _, secret := range []string{c.MySQLPassword, c.SessionSecret} {
		if strings.Contains(captured.String(), secret) {
			t.Fatal("server logs leaked secret")
		}
	}
}
