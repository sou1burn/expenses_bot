package repository

import (
	"context"
	"database/sql"
	"errors"
	"expences_bot/internal/config"
	"fmt"
	"log/slog"
	"strings"
)

func EnsureAppRole(ctx context.Context, cfg config.Config) error {
	if cfg.DbUser == cfg.DbAdminUser {
		return errors.New("DB_USER must differ from DB_ADMIN_USER: app role is stripped of superuser rights")
	}

	admin, err := connect(dbURL(cfg, cfg.DbAdminUser, cfg.DbAdminPassword, "postgres"))
	if err != nil {
		return err
	}
	defer admin.Close()

	const rights = " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD %L"

	var roleExists, dbExists bool
	if err := admin.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1),
		EXISTS (SELECT 1 FROM pg_database WHERE datname = $2)`, cfg.DbUser, cfg.DbName).Scan(&roleExists, &dbExists); err != nil {
		return err
	}

	if roleExists {
		err = execf(ctx, admin, "ALTER ROLE %I WITH"+rights, cfg.DbUser, cfg.DbPassword)
	} else {
		err = execf(ctx, admin, "CREATE ROLE %I"+rights, cfg.DbUser, cfg.DbPassword)
	}
	if err != nil {
		return fmt.Errorf("app role: %w", err)
	}

	if dbExists {
		err = execf(ctx, admin, "ALTER DATABASE %I OWNER TO %I", cfg.DbName, cfg.DbUser)
	} else {
		err = execf(ctx, admin, "CREATE DATABASE %I OWNER %I", cfg.DbName, cfg.DbUser)
	}
	if err != nil {
		return fmt.Errorf("app database: %w", err)
	}
	if err := execf(ctx, admin, "REVOKE ALL ON DATABASE %I FROM PUBLIC", cfg.DbName); err != nil {
		return err
	}

	appDB, err := connect(dbURL(cfg, cfg.DbAdminUser, cfg.DbAdminPassword, cfg.DbName))
	if err != nil {
		return err
	}
	defer appDB.Close()
	if err := execf(ctx, appDB, "ALTER SCHEMA public OWNER TO %I", cfg.DbUser); err != nil {
		return fmt.Errorf("public schema: %w", err)
	}

	slog.Info("app database role ready", "user", cfg.DbUser, "db", cfg.DbName, "created", !roleExists)
	return nil
}

func execf(ctx context.Context, db *sql.DB, tmpl string, args ...string) error {
	params, ph := []any{tmpl}, make([]string, len(args))
	for i, a := range args {
		params = append(params, a)
		ph[i] = fmt.Sprintf("$%d::text", i+2)
	}
	var stmt string
	if err := db.QueryRowContext(ctx, "SELECT format($1::text, "+strings.Join(ph, ", ")+")", params...).Scan(&stmt); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, stmt)
	return err
}
