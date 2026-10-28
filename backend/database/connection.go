// Package database 管理连接和显式建表；不实现登录或上传。
package database

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"time"

	"campusclaw/backend/config"
	"github.com/go-sql-driver/mysql"
)

var ErrUnavailable = errors.New("数据库连接检查失败，请检查MYSQL配置及MySQL运行状态")

func driverConfig(c config.Config) *mysql.Config {
	d := mysql.NewConfig()
	d.Net = "tcp"
	d.Addr = net.JoinHostPort(c.MySQLHost, strconv.Itoa(c.MySQLPort))
	d.User, d.Passwd, d.DBName = c.MySQLUser, c.MySQLPassword, c.MySQLDatabase
	d.ParseTime = true
	d.Timeout, d.ReadTimeout, d.WriteTimeout = 3*time.Second, 3*time.Second, 3*time.Second
	// 驱动内部错误可能含地址等信息，由本模块统一返回安全错误。
	d.Logger = log.New(io.Discard, "", 0)
	return d
}

func Open(ctx context.Context, c config.Config) (*sql.DB, error) {
	connector, err := mysql.NewConnector(driverConfig(c))
	if err != nil {
		return nil, ErrUnavailable
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(3 * time.Minute)
	if err := Check(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func Check(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if db == nil || db.PingContext(ctx) != nil {
		return ErrUnavailable
	}
	var one int
	if db.QueryRowContext(ctx, "SELECT 1").Scan(&one) != nil || one != 1 {
		return ErrUnavailable
	}
	return nil
}
