package main

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

func testEnvironment() map[string]string {
	return map[string]string{"APP_ORIGIN": "http://localhost:5173", "WEB_PORT": "8080", "SESSION_SECRET": "synthetic-test-secret", "MYSQL_HOST": "unreachable.invalid", "MYSQL_PORT": "3306", "MYSQL_DATABASE": "test", "MYSQL_USER": "test", "MYSQL_PASSWORD": "synthetic-test-password"}
}

func TestCheckConfigDoesNotConnectOrLeak(t *testing.T) {
	v := testEnvironment()
	var out, errOut bytes.Buffer
	code := run(context.Background(), func(k string) string { return v[k] }, []string{"--check-config"}, &out, &errOut)
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "尚未连接数据库") {
		t.Fatal("check-only did not complete correctly")
	}
	for _, value := range []string{v["SESSION_SECRET"], v["MYSQL_PASSWORD"], v["MYSQL_HOST"]} {
		if strings.Contains(out.String(), value) {
			t.Fatal("configuration leaked")
		}
	}
}

func TestMissingConfigurationStopsBeforeListen(t *testing.T) {
	var out, errOut bytes.Buffer
	if run(context.Background(), func(string) string { return "" }, nil, &out, &errOut) != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "SESSION_SECRET") {
		t.Fatal("missing configuration did not stop startup")
	}
}

func TestOccupiedPortFailsSafely(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	v := testEnvironment()
	v["API_PORT"] = strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	var out, errOut bytes.Buffer
	if run(context.Background(), func(k string) string { return v[k] }, nil, &out, &errOut) != 1 || !strings.Contains(errOut.String(), "API_PORT") || strings.Contains(errOut.String(), v["MYSQL_PASSWORD"]) {
		t.Fatal("occupied port not handled safely")
	}
}

func TestUnknownArgumentsNotEchoed(t *testing.T) {
	var out, errOut bytes.Buffer
	if run(context.Background(), func(string) string { return "" }, []string{"private-canary"}, &out, &errOut) != 1 || strings.Contains(errOut.String(), "private-canary") {
		t.Fatal("unsafe argument handling")
	}
}
