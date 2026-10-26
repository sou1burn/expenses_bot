package repository

import (
	"context"
	"errors"
	"expences_bot/internal/config"
	"testing"
)

func TestExpensesCRUD(t *testing.T) {
	db, err := NewPostgresDB(*config.LoadConfig("../../.env"))
	if err != nil {
		t.Skip("no database:", err)
	}
	defer db.Close()
	r, ctx := NewPostgresRepo(db), context.Background()
	const uid = -1
	t.Cleanup(func() { db.Exec(`DELETE FROM expenses WHERE user_id = $1`, uid) })

	comment := "Такси до дома"
	taxi, err := r.Create(ctx, uid, 1200, "транспорт", "2026-10-25", &comment)
	if err != nil {
		t.Fatal(err)
	}
	coffee, err := r.Create(ctx, uid, 350, "еда", "2026-10-26", nil)
	if err != nil {
		t.Fatal(err)
	}
	if coffee.Comment != nil || coffee.SpentAt != "2026-10-26" {
		t.Fatalf("create: %+v", coffee)
	}

	if _, err := r.Create(ctx, uid, 1, "кофейни", "2026-10-26", nil); err == nil {
		t.Fatal("unknown category must fail")
	}

	list, err := r.List(ctx, Filter{UserID: uid, From: "2026-10-26", To: "2026-10-26"})
	if err != nil || len(list) != 1 || list[0].ID != coffee.ID {
		t.Fatalf("list by period: %v %+v", err, list)
	}
	list, _ = r.List(ctx, Filter{UserID: uid, Category: "транспорт"})
	if len(list) != 1 || list[0].ID != taxi.ID {
		t.Fatalf("list by category: %+v", list)
	}

	if e, _ := r.FindOwned(ctx, uid, ""); e.ID != coffee.ID {
		t.Fatalf("last: %+v", e)
	}
	if e, _ := r.FindOwned(ctx, uid, "такси"); e.ID != taxi.ID {
		t.Fatalf("by text: %+v", e)
	}
	if _, err := r.FindOwned(ctx, -2, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user must not see: %v", err)
	}

	amount, empty := 450.0, ""
	upd, err := r.Update(ctx, coffee.ID, Patch{Amount: &amount})
	if err != nil || upd.Amount != 450 || upd.Category != "еда" {
		t.Fatalf("patch amount: %v %+v", err, upd)
	}
	upd, _ = r.Update(ctx, taxi.ID, Patch{Comment: &empty})
	if upd.Comment != nil {
		t.Fatalf("comment must be cleared: %+v", upd)
	}

	if err := r.Delete(ctx, coffee.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, coffee.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}
