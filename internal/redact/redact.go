// Package redact removes credential-shaped material from free text before it
// is stored in the ledger, sent to a model, or shown in the UI. It is
// deliberately conservative: it would rather blank a harmless token than let
// a real one through. Tool packages call Redactor.String on every free-text
// field (log lines, event messages, annotations, ConfigMap values).
package redact

import (
	"regexp"
	"strings"
)

// Redactor applies an ordered list of patterns.
type Redactor struct {
	rules []rule
}

type rule struct {
	name string
	re   *regexp.Regexp
	repl string
}

const mask = "[REDACTED]"

// Default returns the standard rule set.
func Default() *Redactor {
	r := &Redactor{}
	add := func(name, pattern, repl string) {
		r.rules = append(r.rules, rule{name: name, re: regexp.MustCompile(pattern), repl: repl})
	}
	// PEM blocks (private keys, certificates with keys).
	add("pem", `(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`, "-----BEGIN PRIVATE KEY-----"+mask+"-----END PRIVATE KEY-----")
	// JWTs.
	add("jwt", `\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`, mask+".jwt")
	// Authorization headers and bearer tokens.
	add("bearer", `(?i)\b(authorization\s*[:=]\s*)?(bearer|basic|token)\s+[A-Za-z0-9._~+/=-]{16,}`, "${1}${2} "+mask)
	// Cloud keys.
	add("aws_access", `\bAKIA[0-9A-Z]{16}\b`, mask+".aws_access_key")
	add("aws_secret", `(?i)\b(aws_secret_access_key|aws_session_token)\b(\s*[:=]\s*)\S+`, "${1}${2}"+mask)
	add("google_api", `\bAIza[0-9A-Za-z_-]{35}\b`, mask+".google_api_key")
	add("github", `\bgh[pousr]_[A-Za-z0-9]{36,}\b`, mask+".github_token")
	add("slack", `\bxox[baprs]-[A-Za-z0-9-]{10,}\b`, mask+".slack_token")
	// Connection strings with embedded credentials.
	add("url_creds", `(?i)\b([a-z][a-z0-9+.-]*://)([^/\s:@]+):([^@\s]+)@`, "${1}${2}:"+mask+"@")
	// key=value / key: value pairs whose key looks secret-bearing.
	add("kv", `(?i)\b([A-Z0-9_.-]*(password|passwd|pwd|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|client[_-]?secret|auth)[A-Z0-9_.-]*)(\s*[:=]\s*)("[^"]*"|'[^']*'|\S+)`, "${1}${3}"+mask)
	// JSON fields with secret-bearing names.
	add("json", `(?i)("(?:[a-z0-9_.-]*(?:password|passwd|secret|token|api_?key|access_?key|private_?key)[a-z0-9_.-]*)"\s*:\s*)"[^"]*"`, "${1}\""+mask+"\"")
	// Long high-entropy hex/base64 blobs (32+ chars) commonly are keys or hashes; keep short prefix.
	add("blob", `\b[A-Fa-f0-9]{40,}\b`, mask+".hex")
	return r
}

// String redacts s.
func (r *Redactor) String(s string) string {
	if r == nil || s == "" {
		return s
	}
	for _, ru := range r.rules {
		s = ru.re.ReplaceAllString(s, ru.repl)
	}
	return s
}

// Func returns String as a plain function for tools.Deps.Redact.
func (r *Redactor) Func() func(string) string { return r.String }

// LooksLikeIdentifier flags high-cardinality label values that resemble
// user or tenant identifiers, so the RCA writer avoids echoing them.
func LooksLikeIdentifier(v string) bool {
	if len(v) < 16 {
		return false
	}
	if strings.Count(v, "-") >= 4 && len(v) >= 32 { // uuid-ish
		return true
	}
	digits := 0
	for _, c := range v {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	return digits*2 > len(v)
}
