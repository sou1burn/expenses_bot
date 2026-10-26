package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

var Categories = []string{"еда", "транспорт", "жильё", "развлечения", "здоровье", "покупки", "другое"}

type Action string

const (
	Add     Action = "add"
	List    Action = "list"
	Update  Action = "update"
	Delete  Action = "delete"
	Unknown Action = "unknown"
)

// Intent — что хочет пользователь.
//   - add:    Amount, Category, SpentAt, Comment
//   - list:   From, To, Category (фильтр)
//   - update: TargetLast/TargetText — какую трату менять; Amount/Category/SpentAt/Comment — новые значения
//   - delete: TargetLast/TargetText
//   - unknown: Reply — что ответить пользователю
type Intent struct {
	Action     Action   `json:"action"`
	Amount     *float64 `json:"amount,omitempty"`
	Category   *string  `json:"category,omitempty"`
	SpentAt    *string  `json:"spent_at,omitempty"` // YYYY-MM-DD
	Comment    *string  `json:"comment,omitempty"`
	From       *string  `json:"from,omitempty"` // YYYY-MM-DD, включительно
	To         *string  `json:"to,omitempty"`   // YYYY-MM-DD, включительно
	TargetLast bool     `json:"target_last,omitempty"`
	TargetText string   `json:"target_text,omitempty"` // искать по comment/category, напр. "такси"
	Reply      string   `json:"reply,omitempty"`
}

type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

type Agent struct {
	cfg  Config
	http *http.Client
	now  func() time.Time
}

func New(cfg Config) *Agent {
	return &Agent{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}, now: time.Now}
}

const systemPrompt = `Ты разбираешь сообщения для бота учёта расходов. Сегодня %s (%s).
Верни ТОЛЬКО JSON-объект без пояснений с полями:
action: "add" | "list" | "update" | "delete" | "unknown"
amount: число (рубли)
category: одна из [%s]
spent_at: дата траты YYYY-MM-DD (по умолчанию сегодня; "вчера", "в пятницу" считай от сегодняшней даты)
comment: на что потрачено, коротко ("кофе", "такси")
from, to: период для list, YYYY-MM-DD включительно ("в этом месяце" = с 1 числа по сегодня, "за неделю" = последние 7 дней)
target_last: true, если речь о последней трате
target_text: слово для поиска траты при update/delete ("удали такси" -> "такси")
reply: для unknown — короткий ответ пользователю на русском
Не указывай поля, о которых в сообщении ничего нет. Для update указывай только новые значения.

Примеры:
"кофе 350" -> {"action":"add","amount":350,"category":"еда","comment":"кофе"}
"такси 1200 вчера" -> {"action":"add","amount":1200,"category":"транспорт","comment":"такси","spent_at":"<вчера>"}
"сколько потратил на еду в этом месяце" -> {"action":"list","category":"еда","from":"<1 число>","to":"<сегодня>"}
"последнее было не 350, а 450" -> {"action":"update","target_last":true,"amount":450}
"удали такси" -> {"action":"delete","target_text":"такси"}
"привет" -> {"action":"unknown","reply":"Напиши трату, например: кофе 350"}`

func (a *Agent) Parse(ctx context.Context, text string) (Intent, error) {
	now := a.now()
	prompt := fmt.Sprintf(systemPrompt, now.Format(time.DateOnly), now.Weekday(), strings.Join(Categories, ", "))

	body, _ := json.Marshal(map[string]any{
		"model":           a.cfg.Model,
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": text},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Intent{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	}

	resp, err := a.http.Do(req)
	if err != nil {
		return Intent{}, fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Intent{}, fmt.Errorf("llm status %d: %.300s", resp.StatusCode, raw)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return Intent{}, fmt.Errorf("llm bad response: %.300s", raw)
	}
	return parseIntent(out.Choices[0].Message.Content, now)
}

func parseIntent(content string, now time.Time) (Intent, error) {
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return Intent{}, fmt.Errorf("no json in llm answer: %.300s", content)
	}
	var in Intent
	if err := json.Unmarshal([]byte(content[start:end+1]), &in); err != nil {
		return Intent{}, fmt.Errorf("llm json: %w", err)
	}

	if in.Category != nil && !slices.Contains(Categories, *in.Category) {
		other := "другое"
		in.Category = &other
	}
	for _, d := range []**string{&in.SpentAt, &in.From, &in.To} {
		if *d != nil {
			if _, err := time.Parse(time.DateOnly, **d); err != nil {
				*d = nil
			}
		}
	}

	switch in.Action {
	case Add:
		if in.Amount == nil || *in.Amount <= 0 {
			return Intent{Action: Unknown, Reply: "Не понял сумму. Пример: кофе 350"}, nil
		}
		if in.Category == nil {
			other := "другое"
			in.Category = &other
		}
		if in.SpentAt == nil {
			today := now.Format(time.DateOnly)
			in.SpentAt = &today
		}
	case List, Update, Delete:
	default:
		in.Action = Unknown
		if in.Reply == "" {
			in.Reply = "Не понял. Пример: «кофе 350», «покажи траты за неделю», «удали такси»"
		}
	}
	return in, nil
}
