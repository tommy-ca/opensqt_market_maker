package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type SlackChannel struct {
	webhookURL string
	client     *http.Client
}

func NewSlackChannel(webhookURL string) *SlackChannel {
	return &SlackChannel{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *SlackChannel) Name() string {
	return "slack"
}

func (s *SlackChannel) Send(ctx context.Context, alert AlertPayload) error {
	if s.webhookURL == "" {
		return nil
	}

	color := "#36a64f" // Green (Info)
	switch alert.Level {
	case Warning:
		color = "#ffcc00" // Yellow
	case Error:
		color = "#ff0000" // Red
	case Critical:
		color = "#8b0000" // Dark Red
	}

	// Format fields
	var fields []map[string]interface{}
	for k, v := range alert.Fields {
		fields = append(fields, map[string]interface{}{
			"title": k,
			"value": v,
			"short": true,
		})
	}

	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color":   color,
				"pretext": fmt.Sprintf("[%s] %s", alert.Level, alert.Title),
				"text":    alert.Message,
				"fields":  fields,
				"ts":      alert.Timestamp.Unix(),
				"footer":  "OpenSQT Market Maker",
			},
		},
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook failed with status: %d", resp.StatusCode)
	}

	return nil
}
