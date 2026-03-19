package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"rag_robot/internal/pkg/config"
)

func NewMySQLDB(cfg config.DatabaseConfig) (*sql.DB, error) {
	if cfg.Charset == "" {
		cfg.Charset = "utf8mb4"
	}
	if cfg.MaxOpenConns <= 0 {
		cfg.MaxOpenConns = 20
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 10
	}
	if cfg.ConnMaxLifetimeMinutes <= 0 {
		cfg.ConnMaxLifetimeMinutes = 60
	}
	if cfg.ConnMaxIdleTimeMinutes <= 0 {
		cfg.ConnMaxIdleTimeMinutes = 30
	}

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local",
		cfg.Username,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
		cfg.Charset,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql failed: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeMinutes) * time.Minute)
	db.SetConnMaxIdleTime(time.Duration(cfg.ConnMaxIdleTimeMinutes) * time.Minute)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql failed: %w", err)
	}

	return db, nil
}
