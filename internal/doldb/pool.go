package doldb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

type DB struct {
	*sql.DB
	cfg    *config.Config
	dolCfg *DolConfig
}

func New(cfg *config.Config) (*DB, error) {
	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	d := &DB{DB: db, cfg: cfg}

	dolCfg, err := d.loadDolConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load dolibarr config: %w", err)
	}
	d.dolCfg = dolCfg

	return d, nil
}

func (d *DB) DolConfig() *DolConfig {
	return d.dolCfg
}

func (d *DB) Prefix() string {
	return d.cfg.DBPrefix
}

func (d *DB) Entity() int {
	return d.cfg.Entity
}

func (d *DB) T(table string) string {
	return d.cfg.T(table)
}
