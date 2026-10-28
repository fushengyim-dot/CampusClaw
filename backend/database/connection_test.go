package database

import (
	"context"
	"net"
	"testing"

	"campusclaw/backend/config"
)

func TestStructuredCredentialsAndBoundedPool(t *testing.T) {
	c := config.Config{MySQLHost: "::1", MySQLPort: 3306, MySQLUser: "test", MySQLPassword: "test-only:@/?&#", MySQLDatabase: "test"}
	d := driverConfig(c)
	if d.Addr != "[::1]:3306" || d.Passwd != c.MySQLPassword || d.DBName != c.MySQLDatabase || d.MultiStatements || d.Timeout <= 0 || d.ReadTimeout <= 0 || d.WriteTimeout <= 0 {
		t.Fatal("unsafe driver configuration")
	}
}

func TestCancelledConnectionReturnsOnlySafeError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db, err := Open(ctx, config.Config{MySQLHost: "private-canary.invalid", MySQLPort: 3306, MySQLUser: "user-canary", MySQLPassword: "password-canary", MySQLDatabase: "db-canary"})
	if db != nil || err != ErrUnavailable {
		t.Fatal("cancelled connection did not return safe error")
	}
}

func TestInvalidMySQLHandshakeReturnsSafeError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := l.Accept()
		if e == nil {
			_, _ = conn.Write([]byte("not-mysql-private-canary"))
			_ = conn.Close()
		}
	}()
	db, err := Open(context.Background(), config.Config{MySQLHost: "127.0.0.1", MySQLPort: l.Addr().(*net.TCPAddr).Port, MySQLUser: "test", MySQLPassword: "test-only", MySQLDatabase: "test"})
	if db != nil || err != ErrUnavailable {
		t.Fatal("invalid handshake not rejected safely")
	}
	<-done
}
