// Package incidents turns the live alert feed into entities with a lifecycle.
//
// An alert from Alertmanager is a question we ask the cluster and forget. An
// incident is something we took responsibility for: it has an identity that
// survives the alert flapping, a state, notes, a priority, and the
// investigations that were run against it.
package incidents

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
)

// keyVersion is baked into every dedup key.
//
// Versioned on purpose. The fields that identify an alert will change — the
// first time somebody discovers that two distinct alerts collide, or that a
// label ought to be part of identity — and without a version every existing
// row silently becomes unreachable. With one, old keys keep matching old rows
// and new keys start a new generation.
const keyVersion = "1"

// Key is the stable identity of an alert across firings.
//
// Name, namespace and resource, and deliberately not the fingerprint: vmalert
// reuses fingerprints across distinct alerts, which is the bug that put
// duplicate React keys on the alert list. Not severity either — an alert that
// escalates from warning to critical is the same alert.
func Key(a monitoring.Alert) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(a.Name)),
		strings.ToLower(strings.TrimSpace(a.Namespace)),
		strings.ToLower(strings.TrimSpace(a.Resource)),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "auto:" + keyVersion + ":" + hex.EncodeToString(sum[:])[:32]
}

// SameAlert reports whether two payloads describe the same thing.
func SameAlert(a, b monitoring.Alert) bool { return Key(a) == Key(b) }
