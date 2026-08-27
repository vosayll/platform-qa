package report

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/url"
	"strings"

	"locali-e2e-engine/config"
	"locali-e2e-engine/pkg/runner"
)

// maxDigestCheckMsgRunes caps the quoted check message in the digest lines.
const maxDigestCheckMsgRunes = 120

// DigestText renders the grouped Telegram digest for a finished batch of runs
// (one regression/webhook group). Failed suites are listed with the title of
// their first failed check and a short reason; an all-green batch gets a
// celebratory line instead. The text is Telegram HTML.
func DigestText(runs []*runner.TestRun, baseURL string) string {
	var b strings.Builder
	b.WriteString("🚀 <b>Регресс завершён</b>\n")
	fmt.Fprintf(&b, "Стенд: %s\n", html.EscapeString(hostOf(baseURL)))

	var failed []*runner.TestRun
	total := 0
	for _, r := range runs {
		if r == nil {
			continue
		}
		total++
		if r.Status == runner.CheckFailed {
			failed = append(failed, r)
		}
	}

	fmt.Fprintf(&b, "Итог: ✅ %d/%d сютов зелёные\n", total-len(failed), total)

	if len(failed) == 0 {
		b.WriteString("Все сюты пройдены 🎉")
		return b.String()
	}

	for _, r := range failed {
		key := r.SuiteKey
		if key == "" {
			key = r.SuiteName
		}
		fmt.Fprintf(&b, "❌ %s — %s\n", html.EscapeString(key), firstFailureLine(r))
	}
	return b.String()
}

// firstFailureLine describes the first failed check of the run:
// «title» (reason up to 120 runes). Falls back to the run-level error when no
// per-check result is available.
func firstFailureLine(run *runner.TestRun) string {
	for _, e := range orderedEntries(run) {
		if e.Status != runner.CheckFailed {
			continue
		}
		title := e.Title
		if title == "" {
			title = e.CheckID
		}
		msg := truncateRunes(e.Message, maxDigestCheckMsgRunes)
		if msg == "" {
			msg = truncateRunes(run.Error, maxDigestCheckMsgRunes)
		}
		if msg == "" {
			return fmt.Sprintf("упал чек «%s»", html.EscapeString(title))
		}
		return fmt.Sprintf("упал чек «%s» (%s)", html.EscapeString(title), html.EscapeString(msg))
	}
	if msg := truncateRunes(run.Error, maxAlertErrorRunes); msg != "" {
		return fmt.Sprintf("ошибка: %s", html.EscapeString(msg))
	}
	return "причина неизвестна"
}

// hostOf returns the host part of the stand base URL for the digest header.
func hostOf(baseURL string) string {
	if baseURL == "" {
		return "не задан"
	}
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return baseURL
}

// SendDigest delivers the batch digest via Telegram. Without a configured bot
// token it is a quiet no-op; delivery problems are logged and swallowed so
// reporting can never affect test execution or panic the server.
func SendDigest(ctx context.Context, cfg *config.Config, text string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[TELEGRAM] digest panic recovered: %v", r)
		}
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil || cfg.TelegramBotToken == "" {
		return
	}
	if cfg.TelegramChatID == "" {
		log.Printf("[TELEGRAM] digest skipped: TELEGRAM_CHAT_ID is empty")
		return
	}

	if err := sendMessage(ctx, cfg, text); err != nil {
		log.Printf("[TELEGRAM] digest send failed: %v", err)
	}
}
