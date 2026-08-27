package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"locali-e2e-engine/config"
	"locali-e2e-engine/pkg/runner"
)

// telegramTimeout bounds a single sendMessage call.
const telegramTimeout = 10 * time.Second

// maxAlertErrorRunes caps the quoted run error in the alert text.
const maxAlertErrorRunes = 300

type telegramPayload struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// NotifyFailure sends a Telegram alert about a failed run. Without configured
// credentials it is a quiet no-op; any delivery problem is logged and
// swallowed so alerting can never affect test execution or panic the server.
func NotifyFailure(ctx context.Context, cfg *config.Config, run *runner.TestRun) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[TELEGRAM] notify panic recovered: %v", r)
		}
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil || cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" || run == nil {
		return
	}

	if err := sendMessage(ctx, cfg, failureText(cfg, run)); err != nil {
		log.Printf("[TELEGRAM] notify failed for run %s: %v", run.ID, err)
	}
}

// sendMessage delivers one HTML message to the configured chat. It returns an
// error on marshalling, network or non-2xx failures; callers decide how to log.
func sendMessage(ctx context.Context, cfg *config.Config, text string) error {
	payload, err := json.Marshal(telegramPayload{
		ChatID:                cfg.TelegramChatID,
		Text:                  text,
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("payload marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.TelegramBotToken),
		bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: telegramTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func failureText(cfg *config.Config, run *runner.TestRun) string {
	var b strings.Builder
	b.WriteString("❌ <b>E2E прогон упал</b>\n")
	fmt.Fprintf(&b, "Сют: %s (%s)\n",
		html.EscapeString(suiteDisplayName(run, run.SuiteKey)),
		html.EscapeString(run.SuiteKey))
	fmt.Fprintf(&b, "Провалено: %d из %d проверок\n", run.FailedChecks, run.TotalChecks)
	if msg := truncateRunes(run.Error, maxAlertErrorRunes); msg != "" {
		fmt.Fprintf(&b, "Ошибка: %s\n", html.EscapeString(msg))
	}
	fmt.Fprintf(&b, "Стенд: %s", html.EscapeString(cfg.BaseURL))
	return b.String()
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
