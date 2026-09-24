package rules

import "strings"

// Channel is where a notification goes.
//
// Discord, Slack and a bare webhook are all "POST JSON to a URL" — the only
// difference is the field the text goes in. Keeping them one type with a kind
// discriminator means adding Teams later is a case in a switch, not a new
// subsystem.
type Channel string

const (
	ChannelSlack   Channel = "slack"
	ChannelDiscord Channel = "discord"
	ChannelWebhook Channel = "webhook"
)

// There is exactly one thing worth sending, and it is not "an alert fired".
//
// Every monitoring tool in existence can already tell you something broke —
// that is the notification people have muted. What nobody else sends is "we
// investigated it, here is the root cause and the first thing to do", which
// arrives while you are still reading the page.
//
// Opened, acknowledged and resolved events were designed and then cut. They
// are cheap to add and they are how a channel becomes noise.

// Notify is one cluster's outbound configuration.
type Notify struct {
	Enabled bool    `json:"enabled"`
	Channel Channel `json:"channel"`
	// URL is the incoming webhook. Never shown back in full once saved.
	URL string `json:"url,omitempty"`
	// MinPriority suppresses anything less urgent. Empty means all.
	MinPriority Priority `json:"minPriority,omitempty"`

	// URLSet and URLHint travel to the browser in place of the secret. A
	// webhook URL is a credential — anyone holding it can post into the
	// channel — so it is never sent back out.
	URLSet  bool   `json:"urlSet,omitempty"`
	URLHint string `json:"urlHint,omitempty"`
	// ClearURL is how the UI asks for the credential to be removed, since an
	// empty URL on save means "unchanged".
	ClearURL bool `json:"clearUrl,omitempty"`
}

// Wants reports whether a finished investigation at this priority should be
// sent. Priority is the only dial, because the event is not in question.
func (n Notify) Wants(p Priority) bool {
	if !n.Enabled || strings.TrimSpace(n.URL) == "" {
		return false
	}
	return n.MinPriority == "" || Rank(p) <= Rank(n.MinPriority)
}

// Redacted returns a copy safe to send to a browser.
//
// A webhook URL is a credential — anyone holding it can post into the channel
// — so it goes out as a hint rather than a value, the same way the Devtron
// token does.
func (n Notify) Redacted() Notify {
	out := n
	if u := strings.TrimSpace(n.URL); u != "" {
		out.URL = ""
		out.URLSet = true
		if i := strings.LastIndex(u, "/"); i >= 0 && i+1 < len(u) {
			tail := u[i+1:]
			if len(tail) > 4 {
				tail = tail[len(tail)-4:]
			}
			out.URLHint = "…" + tail
		}
	}
	return out
}
