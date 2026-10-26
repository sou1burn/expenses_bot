package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("expense not found")

type Expense struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Amount    float64   `json:"amount"`
	Category  string    `json:"category"`
	SpentAt   string    `json:"spent_at"` // YYYY-MM-DD
	Comment   *string   `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Filter struct {
	UserID   int64
	From, To string // YYYY-MM-DD
	Category string
}

type Patch struct {
	UserID   *int64
	Amount   *float64
	Category *string
	SpentAt  *string
	Comment  *string
}

const columns = `id, user_id, amount, category, spent_at::text, comment, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanExpense(s scanner) (Expense, error) {
	var e Expense
	err := s.Scan(&e.ID, &e.UserID, &e.Amount, &e.Category, &e.SpentAt, &e.Comment, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *Repo) List(ctx context.Context, f Filter) ([]Expense, error) {
	var userID any
	if f.UserID != 0 {
		userID = f.UserID
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+columns+` FROM expenses
		WHERE ($1::bigint IS NULL OR user_id = $1)
		  AND ($2::date IS NULL OR spent_at >= $2)
		  AND ($3::date IS NULL OR spent_at <= $3)
		  AND ($4::text IS NULL OR category = $4)
		ORDER BY spent_at DESC, id DESC`,
		userID, nullable(f.From), nullable(f.To), nullable(f.Category))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Expense{}
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func (r *Repo) Create(ctx context.Context, userID int64, amount float64, category, spentAt string, comment *string) (Expense, error) {
	return scanExpense(r.db.QueryRowContext(ctx, `
		INSERT INTO expenses (user_id, amount, category, spent_at, comment)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''))
		RETURNING `+columns,
		userID, amount, category, spentAt, comment))
}

func (r *Repo) Update(ctx context.Context, id int64, p Patch) (Expense, error) {
	return scanExpense(r.db.QueryRowContext(ctx, `
		UPDATE expenses SET
			amount     = COALESCE($2, amount),
			category   = COALESCE($3, category),
			spent_at   = COALESCE($4::date, spent_at),
			comment    = CASE WHEN $5::text IS NULL THEN comment ELSE NULLIF($5, '') END,
			user_id    = COALESCE($6, user_id),
			updated_at = now()
		WHERE id = $1
		RETURNING `+columns,
		id, p.Amount, p.Category, p.SpentAt, p.Comment, p.UserID))
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM expenses WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) FindOwned(ctx context.Context, userID int64, text string) (Expense, error) {
	return scanExpense(r.db.QueryRowContext(ctx, `
		SELECT `+columns+` FROM expenses
		WHERE user_id = $1
		  AND ($2 = '' OR strpos(lower(coalesce(comment, '')), lower($2)) > 0 OR lower(category) = lower($2))
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		userID, text))
}
