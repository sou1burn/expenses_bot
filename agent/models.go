package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
)

// maxModels — лимит OpenRouter на поле "models" в одном запросе.
const maxModels = 3

var preferredVendors = []string{"google/", "meta-llama/", "qwen/", "deepseek/", "mistralai/", "nvidia/"}

type catalogModel struct {
	ID                  string   `json:"id"`
	Created             int64    `json:"created"`
	ContextLength       int      `json:"context_length"`
	SupportedParameters []string `json:"supported_parameters"`
}

func splitModels(s string) []string {
	var out []string
	for _, m := range strings.Split(s, ",") {
		if m = strings.TrimSpace(m); m != "" && m != "auto" {
			out = append(out, m)
		}
	}
	return out
}

// ResolveModels при запуске сверяется с каталогом OpenRouter (GET /models):
// Если каталог недоступен, остаётся список из конфига как есть.
func (a *Agent) ResolveModels(ctx context.Context) error {
	configured := splitModels(a.cfg.Model)
	catalog, err := a.fetchCatalog(ctx)
	if err != nil {
		if len(configured) == 0 {
			return fmt.Errorf("discover free models: %w", err)
		}
		slog.Warn("llm model catalog unavailable, using configured models", "err", err)
		return nil
	}

	available := map[string]bool{}
	for _, m := range catalog {
		available[m.ID] = true
	}
	limit := maxModels
	var models []string
	for _, m := range configured {
		if available[m] {
			models = append(models, m)
		} else {
			slog.Warn("llm model no longer available, skipped", "model", m)
		}
	}
	models = models[:min(len(models), limit)]
	for _, m := range freeModels(catalog) {
		if len(models) >= limit {
			break
		}
		if !slices.Contains(models, m) {
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return errors.New("no free llm models with JSON support found")
	}

	a.models = models
	slog.Info("llm models", "models", models)
	return nil
}

// freeModels: бесплатные модели с поддержкой response_format.
func freeModels(catalog []catalogModel) []string {
	var free []catalogModel
	for _, m := range catalog {
		if strings.HasSuffix(m.ID, ":free") &&
			slices.Contains(m.SupportedParameters, "response_format") &&
			m.ContextLength >= 32000 &&
			!strings.Contains(m.ID, "safety") && !strings.Contains(m.ID, "guard") {
			free = append(free, m)
		}
	}
	vendorRank := func(id string) int {
		for i, v := range preferredVendors {
			if strings.HasPrefix(id, v) {
				return i
			}
		}
		return len(preferredVendors)
	}
	slices.SortStableFunc(free, func(x, y catalogModel) int {
		return cmp.Or(cmp.Compare(vendorRank(x.ID), vendorRank(y.ID)), cmp.Compare(y.Created, x.Created))
	})

	var ids, rest []string
	seen := map[string]bool{}
	for _, m := range free {
		vendor, _, _ := strings.Cut(m.ID, "/")
		if seen[vendor] {
			rest = append(rest, m.ID)
		} else {
			seen[vendor] = true
			ids = append(ids, m.ID)
		}
	}
	return append(ids, rest...)
}

func (a *Agent) fetchCatalog(ctx context.Context) ([]catalogModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var out struct {
		Data []catalogModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
