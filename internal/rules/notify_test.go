package rules

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWants(t *testing.T) {
	t.Parallel()

	on := Notify{Enabled: true, URL: "https://hooks.example/x"}

	tests := []struct {
		name     string
		n        Notify
		priority Priority
		want     bool
	}{
		{"enabled with a url sends", on, P1, true},
		{"disabled never sends", Notify{URL: "https://hooks.example/x"}, P0, false},
		{"no url never sends", Notify{Enabled: true}, P0, false},
		{"whitespace is not a url", Notify{Enabled: true, URL: "   "}, P0, false},
		{
			"minPriority P0 drops P1",
			Notify{Enabled: true, URL: "u", MinPriority: P0},
			P1,
			false,
		},
		{
			"minPriority P0 keeps P0",
			Notify{Enabled: true, URL: "u", MinPriority: P0},
			P0,
			true,
		},
		{
			"minPriority P1 keeps the more urgent P0",
			Notify{Enabled: true, URL: "u", MinPriority: P1},
			P0,
			true,
		},
		{"no minPriority means all", on, P2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.n.Wants(tt.priority); got != tt.want {
				t.Errorf("Wants(%s) = %v, want %v", tt.priority, got, tt.want)
			}
		})
	}
}

// A webhook URL is a credential. It must never travel back to a browser.
func TestRedacted(t *testing.T) {
	t.Parallel()

	n := Notify{Enabled: true, URL: "https://hooks.slack.com/services/T00/B11/abcdefgh"}
	r := n.Redacted()

	if r.URL != "" {
		t.Errorf("the url must never be sent back, got %q", r.URL)
	}
	if !r.URLSet {
		t.Error("the UI still has to know one is configured")
	}
	if r.URLHint != "…efgh" {
		t.Errorf("want a short tail to recognise it by, got %q", r.URLHint)
	}
	if strings.Contains(r.URLHint, "T00") || strings.Contains(r.URLHint, "B11") {
		t.Error("the hint must not carry enough to reconstruct the url")
	}

	// Nothing configured stays nothing.
	if e := (Notify{}).Redacted(); e.URLSet || e.URLHint != "" {
		t.Error("an unconfigured channel must not claim to be set")
	}
}

func TestSendShapesPerChannel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		channel Channel
		field   string
	}{
		{ChannelSlack, "text"},
		{ChannelDiscord, "content"},
		{ChannelWebhook, "title"},
		{"", "text"}, // unset behaves as Slack
	}

	for _, tt := range tests {
		t.Run(string(tt.channel)+"/"+tt.field, func(t *testing.T) {
			t.Parallel()

			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &got)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			err := Send(context.Background(),
				Notify{Enabled: true, Channel: tt.channel, URL: srv.URL},
				Message{Title: "root cause found", Body: "pgvector never completes initdb", Priority: P0},
				srv.Client())
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			if _, ok := got[tt.field]; !ok {
				t.Errorf("want field %q for %s, got %v", tt.field, tt.channel, got)
			}
		})
	}
}

func TestSendReportsFailure(t *testing.T) {
	t.Parallel()

	// A 4xx from Slack means the message did not arrive. Swallowing it would
	// leave someone believing a channel works when it does not.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := Send(context.Background(),
		Notify{Enabled: true, Channel: ChannelSlack, URL: srv.URL},
		Message{Title: "x"}, srv.Client())
	if err == nil {
		t.Fatal("a 404 must be reported, not swallowed")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("want the status in the error, got %v", err)
	}

	if err := Send(context.Background(), Notify{Enabled: true}, Message{}, nil); err == nil {
		t.Error("an empty url must be an error")
	}
}

// Discord rejects a body over 2000 characters outright rather than
// truncating, so a long root cause would silently fail to deliver.
func TestDiscordIsClipped(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 5000)
	p, ok := payload(ChannelDiscord, Message{Title: long}).(map[string]any)
	if !ok {
		t.Fatal("unexpected payload shape")
	}
	if n := len([]rune(p["content"].(string))); n > 2000 {
		t.Errorf("discord payload is %d runes, which the API will reject", n)
	}
}
