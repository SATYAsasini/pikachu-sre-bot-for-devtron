package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Message is one notification, before it is shaped for a particular channel.
type Message struct {
	Title string
	Body  string
	// Priority colours the message where the channel supports it.
	Priority Priority
	// Link is the run or alert this is about.
	Link string
}

// Send posts a message to the configured channel.
//
// One function, three shapes. Slack and Discord both accept a JSON body with
// a text field and differ mainly in what that field is called; a bare webhook
// gets the structured message so whatever is on the other end can do its own
// formatting rather than parse prose back out.
func Send(ctx context.Context, n Notify, m Message, client *http.Client) error {
	url := strings.TrimSpace(n.URL)
	if url == "" {
		return fmt.Errorf("no webhook url configured")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	body, err := json.Marshal(payload(n.Channel, m))
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Slack answers 200 with the body "ok"; Discord answers 204. Anything in
	// the 2xx range is a delivery.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s returned %d", n.Channel, resp.StatusCode)
	}
	return nil
}

// payload shapes the message for one channel.
func payload(c Channel, m Message) any {
	text := m.Title
	if m.Body != "" {
		text += "\n" + m.Body
	}
	if m.Link != "" {
		text += "\n" + m.Link
	}

	switch c {
	case ChannelDiscord:
		// Discord rejects content over 2000 characters outright rather than
		// truncating, so a long root cause would silently fail to deliver.
		return map[string]any{"content": clip(text, 1900)}
	case ChannelWebhook:
		// Structured, because the far end is code rather than a person.
		return map[string]any{
			"title":    m.Title,
			"body":     m.Body,
			"priority": string(m.Priority),
			"link":     m.Link,
		}
	default: // Slack, and anything unset
		return map[string]any{"text": clip(text, 3000)}
	}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
