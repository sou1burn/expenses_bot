package handler

import (
	"encoding/json"
	"errors"
	"expences_bot/internal/repository"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

type Handler struct {
	repo *repository.Repo
}

func NewHandler(repo *repository.Repo) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/expenses", h.List)
	mux.HandleFunc("POST /api/expenses", h.Create)
	mux.HandleFunc("PUT /api/expenses/{id}", h.Update)
	mux.HandleFunc("DELETE /api/expenses/{id}", h.Delete)
}

type expenseBody struct {
	UserID   int64   `json:"user_id"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
	SpentAt  string  `json:"spent_at"`
	Comment  *string `json:"comment"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("Failed to encode JSON response", "error", err)
	}
}

func writeErr(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, repository.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.As(err, &pgErr) && (pgErr.Code[:2] == "22" || pgErr.Code[:2] == "23"):
		http.Error(w, pgErr.Message, http.StatusBadRequest)
	default:
		slog.Error("request failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
	}
	return id, err == nil
}

func decodeBody(w http.ResponseWriter, r *http.Request) (expenseBody, bool) {
	var b expenseBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "incorrect JSON: "+err.Error(), http.StatusBadRequest)
		return b, false
	}
	if b.UserID <= 0 || b.SpentAt == "" || b.Category == "" {
		http.Error(w, "user_id, category and spent_at are required", http.StatusBadRequest)
		return b, false
	}
	return b, true
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repository.Filter{From: q.Get("from"), To: q.Get("to"), Category: q.Get("category")}
	if s := q.Get("user_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			http.Error(w, "bad user_id", http.StatusBadRequest)
			return
		}
		f.UserID = id
	}
	list, err := h.repo.List(r.Context(), f)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	b, ok := decodeBody(w, r)
	if !ok {
		return
	}
	e, err := h.repo.Create(r.Context(), b.UserID, b.Amount, b.Category, b.SpentAt, b.Comment)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, ok := decodeBody(w, r)
	if !ok {
		return
	}
	comment := ""
	if b.Comment != nil {
		comment = *b.Comment
	}
	e, err := h.repo.Update(r.Context(), id, repository.Patch{
		UserID: &b.UserID, Amount: &b.Amount, Category: &b.Category, SpentAt: &b.SpentAt, Comment: &comment,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
