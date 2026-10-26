package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
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
