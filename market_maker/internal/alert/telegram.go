package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type TelegramChannel struct {
	botToken string
	chatID   string
	client   *http.Client
}

func NewTelegramChannel(botToken, chatID string) *TelegramChannel {
	return &TelegramChannel{
		botToken: botToken,
		chatID:   chatID,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (t *TelegramChannel) Name() string {
	return "telegram"
}

func (t *TelegramChannel) Send(ctx context.Context, alert AlertPayload) error {
	if t.botToken == "" || t.chatID == "" {
		return nil
	}

	icon := "ℹ️"
	switch alert.Level {
	case Warning:
		icon = "⚠️"
	case Error:
		icon = "❌"
	case Critical:
		icon = "🚨"
	}

	text := fmt.Sprintf("%s *[%s] %s*\n\n%s", icon, alert.Level, alert.Title, alert.Message)
	if len(alert.Fields) > 0 {
		text += "\n"
		for k, v := range alert.Fields {
			text += fmt.Sprintf("\n- *%s*: %s", k, v)
		}
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)
	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram api failed with status: %d", resp.StatusCode)
	}

	return nil
}
