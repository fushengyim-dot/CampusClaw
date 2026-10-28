package config

import (
	"strings"
	"testing"
)

func sample() map[string]string {
	return map[string]string{"APP_ORIGIN": "http://127.0.0.1:5173", "WEB_PORT": "8080", "SESSION_SECRET": "unit-test-only-not-a-real-secret", "MYSQL_HOST": "127.0.0.1", "MYSQL_PORT": "3306", "MYSQL_DATABASE": "unit_test", "MYSQL_USER": "unit_test", "MYSQL_PASSWORD": " unit-test-only-password "}
}

func TestValidConfigPreservesPassword(t *testing.T) {
	v := sample()
	c, err := Load(func(k string) string { return v[k] })
	if err != nil {
		t.Fatal("valid configuration rejected")
	}
	if c.APIPort != 18080 || c.MySQLPort != 3306 || c.MySQLPassword != v["MYSQL_PASSWORD"] {
		t.Fatal("defaults or password preservation failed")
	}
}

func TestMissingRequiredVariables(t *testing.T) {
	for key := range sample() {
		t.Run(key, func(t *testing.T) {
			v := sample()
			v[key] = " \t"
			_, err := Load(func(k string) string { return v[k] })
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatal("missing variable not identified")
			}
			if strings.Contains(err.Error(), v["SESSION_SECRET"]) || strings.Contains(err.Error(), "unit-test-only-password") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestInvalidFormatsNeverEchoValues(t *testing.T) {
	cases := []struct{ key, value string }{
		{"WEB_PORT", "0"}, {"WEB_PORT", "65536"}, {"MYSQL_PORT", "abc-private-value"}, {"API_PORT", "-1"}, {"API_PORT", " 18080"},
		{"APP_ORIGIN", "file:///private"}, {"APP_ORIGIN", "https://user:private@example.com"}, {"APP_ORIGIN", "https://example.com/"}, {"APP_ORIGIN", "https://example.com?private=value"}, {"APP_ORIGIN", "http://localhost:99999"}, {"APP_ORIGIN", "http://localhost:"}, {"MYSQL_HOST", "private\nvalue"},
	}
	for i, tc := range cases {
		t.Run(tc.key+string(rune('A'+i)), func(t *testing.T) {
			v := sample()
			v[tc.key] = tc.value
			_, err := Load(func(k string) string { return v[k] })
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatal("invalid value accepted")
			}
			if strings.Contains(err.Error(), tc.value) {
				t.Fatal("invalid value leaked")
			}
		})
	}
}
