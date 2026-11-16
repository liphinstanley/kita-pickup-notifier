package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
)

type Message struct {
	Title  string
	Body   string
	Urgent bool
	Link   string // shown as a "Directions" button
}

// telegramText renders the message as Telegram HTML (bold title, plain body).
func telegramText(m Message) string {
	icon := "🚋"
	if m.Urgent {
		icon = "🚨"
	}
	return fmt.Sprintf("%s <b>%s</b>\n%s", icon, html.EscapeString(m.Title), html.EscapeString(m.Body))
}

// sendTelegram delivers m to every chat in cfg.TelegramChats via the Bot API.
// Bot API limits are per bot (~30 msg/s), not per source IP, so Cloud Run's
// shared egress IPs are fine.
func sendTelegram(ctx context.Context, c *http.Client, cfg Config, m Message) error {
	for _, chat := range cfg.TelegramChats {
		payload := map[string]any{
			"chat_id":                  chat,
			"text":                     telegramText(m),
			"parse_mode":               "HTML",
			"disable_web_page_preview": true,
		}
		if m.Link != "" {
			payload["reply_markup"] = map[string]any{
				"inline_keyboard": [][]map[string]string{{{"text": "Directions", "url": m.Link}}},
			}
		}
		body, _ := json.Marshal(payload)
		url := "https://api.telegram.org/bot" + cfg.TelegramToken + "/sendMessage"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			// the URL contains the token; never log it
			return fmt.Errorf("telegram send to %s: request failed", chat)
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("telegram send to %s: HTTP %d: %s", chat, resp.StatusCode, strings.TrimSpace(string(b)))
		}
	}
	return nil
}
