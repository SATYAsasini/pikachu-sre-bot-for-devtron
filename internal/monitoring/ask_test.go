package monitoring

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAlertCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		alert Alert
		want  string
	}{
		{"reason wins", fxCrashLoop(), "CrashLoopBackOff"},
		{"phase when there is no reason", fxPhaseOnly(), "Pending"},
		{"blank condition labels fall through to the alert name", fxBlankCondition(), "TargetDown"},
		{"no labels at all falls back to the alert name", fxNoResource(), "KubeSchedulerDown"},
		{"empty alert yields an empty condition", fxEmpty(), ""},
		{"unicode passes through", fxUnicode(), "メモリ不足"},
		{
			"reason is preferred over phase",
			Alert{Labels: map[string]string{"phase": "Pending", "reason": "Evicted"}},
			"Evicted",
		},
		{
			"condition and status are used when nothing better exists",
			Alert{Labels: map[string]string{"status": "Degraded"}},
			"Degraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.alert.Condition(); got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestAlertAsk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		alert  Alert
		want   string
		absent []string
	}{
		{
			name:  "names the resource and the state it is in",
			alert: fxCrashLoop(),
			want:  "Why is pod pgvector-6cccdcb6f6-cgdhj in namespace devtroncd in CrashLoopBackOff? Find the root cause and suggest a fix.",
			// The alert's paperwork stays out of the question: it is already in
			// the structured context and in our own fact pack, and sending it
			// made the first pass spend its budget restating the input.
			absent: []string{"critical", "severity", "2026-09-23", "Labels:", "kube_pod_container_status", "Summary:", "restarted 540"},
		},
		{
			name:  "falls back to the alert name when no object resolved",
			alert: fxNoResource(),
			want:  "Why is the resource behind alert KubeSchedulerDown in KubeSchedulerDown? Find the root cause and suggest a fix.",
		},
		{
			name:  "uses phase when there is no reason",
			alert: fxPhaseOnly(),
			want:  "Why is pod api-7d9f in namespace prod in Pending? Find the root cause and suggest a fix.",
		},
		{
			name:  "kind is lowercased for prose",
			alert: fxBlankCondition(),
			want:  "Why is service node-exporter in namespace monitoring in TargetDown? Find the root cause and suggest a fix.",
		},
		{
			name:  "unicode survives intact",
			alert: fxUnicode(),
			want:  "Why is pod サービス-01 in namespace 本番 in メモリ不足? Find the root cause and suggest a fix.",
		},
		{
			name:  "an empty alert still asks a well-formed question",
			alert: fxEmpty(),
			want:  "Why is the resource behind alert  in ? Find the root cause and suggest a fix.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.alert.Ask()
			if got != tt.want {
				t.Errorf("want:\n%q\ngot:\n%q", tt.want, got)
			}
			for _, no := range tt.absent {
				if strings.Contains(got, no) {
					t.Errorf("want %q absent from the ask, got:\n%s", no, got)
				}
			}
			if !utf8.ValidString(got) {
				t.Error("ask is not valid utf-8")
			}
			// One question and one instruction. If this grows again, the
			// preamble has crept back.
			if n := strings.Count(got, "\n"); n != 0 {
				t.Errorf("want a single line, got %d newlines:\n%s", n, got)
			}
		})
	}
}
