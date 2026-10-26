package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("no auth header")
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"` +
			"```json\\n{\\\"action\\\":\\\"add\\\",\\\"amount\\\":350,\\\"category\\\":\\\"кофейни\\\",\\\"comment\\\":\\\"кофе\\\",\\\"spent_at\\\":\\\"вчера\\\"}\\n```" +
			`"}}]}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	a.now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }

	in, err := a.Parse(context.Background(), "кофе 350")
	if err != nil {
		t.Fatal(err)
	}
	if in.Action != Add || *in.Amount != 350 || *in.Category != "другое" || *in.SpentAt != "2026-10-25" || *in.Comment != "кофе" {
		t.Fatalf("got %+v", in)
	}

	if in, _ := parseIntent(`{"action":"add"}`, time.Now()); in.Action != Unknown {
		t.Fatalf("add without amount must be unknown, got %+v", in)
	}
}

func TestResolveModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[
			{"id":"acme/new:free","created":300,"context_length":128000,"supported_parameters":["response_format"]},
			{"id":"google/gemma:free","created":100,"context_length":128000,"supported_parameters":["response_format"]},
			{"id":"google/no-json:free","created":400,"context_length":128000,"supported_parameters":["tools"]},
			{"id":"nvidia/safety:free","created":500,"context_length":128000,"supported_parameters":["response_format"]},
			{"id":"qwen/tiny:free","created":500,"context_length":8000,"supported_parameters":["response_format"]},
			{"id":"openai/paid","created":500,"context_length":128000,"supported_parameters":["response_format"]},
			{"id":"qwen/big:free","created":200,"context_length":128000,"supported_parameters":["response_format"]},
			{"id":"openrouter/free","created":1,"context_length":128000,"supported_parameters":["response_format"]}
		]}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Model: "openai/paid, gone/model:free"})
	if err := a.ResolveModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	// явная модель сохранена, пропавшая выкинута, остальное — бесплатные с JSON, предпочтительные вендоры первыми
	if got := strings.Join(a.models, ","); got != "openai/paid,google/gemma:free,qwen/big:free" {
		t.Fatalf("models = %s", got)
	}

	a = New(Config{BaseURL: srv.URL, Model: "auto"})
	a.ResolveModels(context.Background())
	if got := strings.Join(a.models, ","); got != "google/gemma:free,qwen/big:free,acme/new:free" {
		t.Fatalf("auto models = %s", got)
	}
}

func TestParseRetriesUnavailable(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"delete\",\"target_text\":\"такси\"}"}}]}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Model: "m"})
	a.retryDelay = 0
	in, err := a.Parse(context.Background(), "удали такси")
	if err != nil || in.Action != Delete || calls != 3 {
		t.Fatalf("calls=%d err=%v in=%+v", calls, err, in)
	}

	calls = -10 // всегда 502
	if _, err := a.Parse(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
