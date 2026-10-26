package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"expences_bot/agent"
	"expences_bot/internal/repository"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Bot struct {
	api   string
	http  *http.Client
	repo  *repository.Repo
	agent *agent.Agent
}

func New(token string, repo *repository.Repo, ag *agent.Agent) *Bot {
	return &Bot{
		api:   "https://api.telegram.org/bot" + token + "/",
		http:  &http.Client{Timeout: 60 * time.Second},
		repo:  repo,
		agent: ag,
	}
}

type message struct {
	Text string `json:"text"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
}

func (b *Bot) Run(ctx context.Context) {
	offset := 0
	for ctx.Err() == nil {
		var updates []struct {
			UpdateID int      `json:"update_id"`
			Message  *message `json:"message"`
		}
		q := url.Values{"offset": {strconv.Itoa(offset)}, "timeout": {"30"}, "allowed_updates": {`["message"]`}}
		if err := b.call(ctx, "getUpdates?"+q.Encode(), nil, &updates); err != nil {
			if ctx.Err() == nil {
				slog.Error("telegram getUpdates", "err", err)
				time.Sleep(3 * time.Second)
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message != nil && u.Message.Text != "" {
				b.send(ctx, u.Message.Chat.ID, b.reply(ctx, u.Message.From.ID, u.Message.Text))
			}
		}
	}
}

func (b *Bot) call(ctx context.Context, method string, body any, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.api+method, nil)
	if body != nil {
		data, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, b.api+method, bytes.NewReader(data))
	}
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), b.api, "telegram/")) // не светим токен в логах
	}
	defer resp.Body.Close()

	var res struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("telegram %s: %s", strings.SplitN(method, "?", 2)[0], res.Description)
	}
	if out != nil {
		return json.Unmarshal(res.Result, out)
	}
	return nil
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) {
	if err := b.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, nil); err != nil {
		slog.Error("telegram sendMessage", "err", err)
	}
}

func (b *Bot) reply(ctx context.Context, userID int64, text string) string {
	if strings.HasPrefix(text, "/start") {
		return "Привет! Пиши траты обычным текстом, например: " +
			"\n кофе 350" +
			"\n такси 1200 вчера" +
			"\n сколько потратил на еду в этом месяце" +
			"\n последнее было не 350, а 450" +
			"\n удали такси"
	}

	in, err := b.agent.Parse(ctx, text)
	if errors.Is(err, agent.ErrUnavailable) {
		slog.Warn("agent unavailable", "err", err)
		return "Модели сейчас перегружены, попробуй через минуту."
	}
	if err != nil {
		slog.Error("agent parse", "err", err)
		return "Не получилось разобрать сообщение, попробуй ещё раз."
	}

	switch in.Action {
	case agent.Add:
		e, err := b.repo.Create(ctx, userID, *in.Amount, *in.Category, *in.SpentAt, in.Comment)
		if err != nil {
			return b.fail(err)
		}
		return "Записал: " + describe(e)

	case agent.List:
		f := repository.Filter{UserID: userID}
		if in.From != nil {
			f.From = *in.From
		}
		if in.To != nil {
			f.To = *in.To
		}
		if in.Category != nil {
			f.Category = *in.Category
		}
		list, err := b.repo.List(ctx, f)
		if err != nil {
			return b.fail(err)
		}
		return summary(list, f)

	case agent.Update, agent.Delete:
		e, err := b.repo.FindOwned(ctx, userID, in.TargetText)
		if errors.Is(err, repository.ErrNotFound) {
			return "Не нашёл такую трату."
		}
		if err != nil {
			return b.fail(err)
		}
		if in.Action == agent.Delete {
			if err := b.repo.Delete(ctx, e.ID); err != nil {
				return b.fail(err)
			}
			return "Удалил: " + describe(e)
		}
		p := repository.Patch{Amount: in.Amount, Category: in.Category, SpentAt: in.SpentAt, Comment: in.Comment}
		if p == (repository.Patch{}) {
			return "Не понял, что изменить. Пример: последнее было не 350, а 450"
		}
		upd, err := b.repo.Update(ctx, e.ID, p)
		if err != nil {
			return b.fail(err)
		}
		return "Исправил: " + describe(upd)

	default:
		return in.Reply
	}
}

func (b *Bot) fail(err error) string {
	slog.Error("bot repo", "err", err)
	return "Что-то пошло не так, попробуй позже."
}

func money(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64) + " ₽"
}

func shortDate(iso string) string {
	if t, err := time.Parse(time.DateOnly, iso); err == nil {
		return t.Format("02.01")
	}
	return iso
}

func describe(e repository.Expense) string {
	what := e.Category
	if e.Comment != nil {
		what = *e.Comment + " (" + e.Category + ")"
	}
	return fmt.Sprintf("%s — %s, %s", what, money(e.Amount), shortDate(e.SpentAt))
}

const maxListed = 20

func summary(list []repository.Expense, f repository.Filter) string {
	if len(list) == 0 {
		return "Трат не найдено."
	}
	var total float64
	for _, e := range list {
		total += e.Amount
	}

	var sb strings.Builder
	sb.WriteString("Итого")
	if f.Category != "" {
		sb.WriteString(" на " + f.Category)
	}
	switch {
	case f.From != "" && f.To != "":
		sb.WriteString(fmt.Sprintf(" с %s по %s", shortDate(f.From), shortDate(f.To)))
	case f.From != "":
		sb.WriteString(" с " + shortDate(f.From))
	case f.To != "":
		sb.WriteString(" по " + shortDate(f.To))
	}
	sb.WriteString(fmt.Sprintf(": %s (%d шт.)\n", money(total), len(list)))

	for i, e := range list {
		if i == maxListed {
			sb.WriteString(fmt.Sprintf("\n…и ещё %d", len(list)-maxListed))
			break
		}
		sb.WriteString("\n• " + describe(e))
	}
	return sb.String()
}
