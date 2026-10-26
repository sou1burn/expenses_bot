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
	"github.com/jackc/pgx/v5/pgconn"
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

func dbURL(cfg config.Config, user, password, dbName string) url.URL {
	return url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     cfg.DbHost + ":" + cfg.DbPort,
		Path:     dbName,
		RawQuery: "sslmode=disable",
	}
}

func connect(u url.URL) (*sql.DB, error) {
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	for i := range 10 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = db.PingContext(ctx)
		cancel()
		var pgErr *pgconn.PgError
		if err == nil || errors.As(err, &pgErr) && pgErr.Code[:2] == "28" {
			break
		}
		slog.Warn("database not ready", "attempt", i+1, "err", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("connect db as %s: %w", u.User.Username(), err)
	}
	return db, nil
}

func NewPostgresDB(cfg config.Config) (*sql.DB, error) {
	u := dbURL(cfg, cfg.DbUser, cfg.DbPassword, cfg.DbName)
	db, err := connect(u)
	if err != nil {
		return nil, err
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
