package repository

import (
	"context"
	"database/sql"
	"errors"
	"expences_bot/internal/config"
	"expences_bot/migrations"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Repo struct {
	db *sql.DB
}

func NewPostgresRepo(db *sql.DB) *Repo {
	return &Repo{
		db: db,
	}
}

func NewPostgresDB(cfg config.Config) (*sql.DB, error) {
	u := url.URL{
		User:     url.UserPassword(cfg.DbUser, cfg.DbPassword),
		Host:     cfg.DbHost + ":" + cfg.DbPort,
		Path:     cfg.DbName,
		RawQuery: "sslmode=disable",
	}

	u.Scheme = "postgres"
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	for i := range 10 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			break
		}
		slog.Warn("database not ready", "attempt", i+1, "err", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("connect db: %w", err)
	}
	slog.Info("connected to PostgreSQL")

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(10 * time.Minute)

	u.Scheme = "pgx5"
	if err := migrateUp(u.String()); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateUp(dbURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("migrations source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dbURL)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	v, dirty, _ := m.Version()
	slog.Info("migrations applied", "version", v, "dirty", dirty)
	return nil
}
