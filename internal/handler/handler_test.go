package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestProtection(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/expenses", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("POST /api/expenses", (&Handler{}).Create) // до репозитория не доходит: отказ раньше
	h := http.NewCrossOriginProtection().Handler(mux)         // как в cmd/main.go

	cases := []struct {
		name, method, host, ctype string
		headers                   map[string]string
		want                      int
	}{
		{"local GET", "GET", "localhost:8080", "", nil, 200},
		{"ipv6 GET", "GET", "[::1]:8080", "", nil, 200},
		{"GET by IP", "GET", "192.168.1.10:8080", "", nil, 200},
		{"curl POST without Content-Type", "POST", "localhost:8080", "", nil, 400},
		{"cross-site POST", "POST", "localhost:8080", "application/json", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"cross-origin POST", "POST", "localhost:8080", "application/json", map[string]string{"Origin": "https://evil.example"}, 403},
		{"huge body", "POST", "localhost:8080", "application/json", nil, 400},
	}
	for _, c := range cases {
		body := `{}`
		if c.name == "huge body" {
			body = `{"comment":"` + strings.Repeat("a", maxBodyBytes) + `"}`
		}
		r := httptest.NewRequest(c.method, "/api/expenses", strings.NewReader(body))
		r.Host = c.host
		if c.ctype != "" {
			r.Header.Set("Content-Type", c.ctype)
		}
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d (%s)", c.name, w.Code, c.want, strings.TrimSpace(w.Body.String()))
		}
	}
}
