// Package config 只读取进程环境，不读取.env文件，也不连接数据库。
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppOrigin, SessionSecret                           string
	MySQLHost, MySQLDatabase, MySQLUser, MySQLPassword string
	MySQLPort, WebPort, APIPort                        int
	APIListenHost                                      string
}

func port(value string) (int, bool) {
	n, err := strconv.Atoi(value)
	return n, err == nil && n > 0 && n <= 65535 && strconv.Itoa(n) == value
}

// Load的错误仅由固定变量名组成，绝不拼接用户提供的值。
func Load(getenv func(string) string) (Config, error) {
	keys := []string{"APP_ORIGIN", "WEB_PORT", "MYSQL_HOST", "MYSQL_PORT", "MYSQL_DATABASE", "MYSQL_USER"}
	values := make(map[string]string)
	var invalid []string
	for _, key := range keys {
		values[key] = getenv(key)
		if strings.TrimSpace(values[key]) == "" {
			invalid = append(invalid, key)
		}
	}
	for _, key := range []string{"SESSION_SECRET", "MYSQL_PASSWORD"} {
		value, err := configuredSecret(getenv, key)
		if err != nil || strings.TrimSpace(value) == "" {
			invalid = append(invalid, key)
		} else {
			values[key] = value
		}
	}
	if len(invalid) > 0 {
		return Config{}, fmt.Errorf("缺少必需配置：%s", strings.Join(invalid, ", "))
	}
	c := Config{AppOrigin: values["APP_ORIGIN"], SessionSecret: values["SESSION_SECRET"], MySQLHost: values["MYSQL_HOST"], MySQLDatabase: values["MYSQL_DATABASE"], MySQLUser: values["MYSQL_USER"], MySQLPassword: values["MYSQL_PASSWORD"]}
	u, err := url.Parse(c.AppOrigin)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(c.AppOrigin, " \t\r\n#") {
		invalid = append(invalid, "APP_ORIGIN")
	} else if strings.HasSuffix(u.Host, ":") {
		invalid = append(invalid, "APP_ORIGIN")
	} else if u.Port() != "" {
		if _, ok := port(u.Port()); !ok {
			invalid = append(invalid, "APP_ORIGIN")
		}
	}
	var ok bool
	if c.WebPort, ok = port(values["WEB_PORT"]); !ok {
		invalid = append(invalid, "WEB_PORT")
	}
	if c.MySQLPort, ok = port(values["MYSQL_PORT"]); !ok {
		invalid = append(invalid, "MYSQL_PORT")
	}
	apiPort := getenv("API_PORT")
	if apiPort == "" {
		apiPort = "18080"
	}
	if c.APIPort, ok = port(apiPort); !ok {
		invalid = append(invalid, "API_PORT")
	}
	c.APIListenHost = getenv("API_LISTEN_HOST")
	if c.APIListenHost == "" {
		c.APIListenHost = "127.0.0.1"
	}
	if c.APIListenHost != "127.0.0.1" && c.APIListenHost != "0.0.0.0" {
		invalid = append(invalid, "API_LISTEN_HOST")
	}
	for _, key := range []string{"MYSQL_HOST", "MYSQL_DATABASE", "MYSQL_USER"} {
		if strings.ContainsAny(values[key], " \t\r\n\x00") {
			invalid = append(invalid, key)
		}
	}
	if len(invalid) > 0 {
		return Config{}, fmt.Errorf("配置格式错误：%s", strings.Join(invalid, ", "))
	}
	return c, nil
}

func configuredSecret(getenv func(string) string, name string) (string, error) {
	value, file := getenv(name), getenv(name+"_FILE")
	if value != "" && file != "" {
		return "", fmt.Errorf("ambiguous %s", name)
	}
	if file == "" {
		return value, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}
