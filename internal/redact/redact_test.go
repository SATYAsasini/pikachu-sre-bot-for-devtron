package redact

import (
	"strings"
	"testing"
)

func TestRedactor(t *testing.T) {
	r := Default()
	cases := []struct{ in, mustNotContain string }{
		{`Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`, "dozjgNryP4J3"},
		{`DATABASE_URL=postgres://payments:Sup3rS3cret@db.internal:5432/pay`, "Sup3rS3cret"},
		{`PAYMENT_GW_API_KEY=sk_live_51H8xk2eZvKYlo2C`, "sk_live_51H8xk2eZvKYlo2C"},
		{`{"password":"hunter22","user":"bob"}`, "hunter22"},
		{`aws_access_key_id = AKIAIOSFODNN7EXAMPLE`, "AKIAIOSFODNN7EXAMPLE"},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----", "MIIEowIBAAKCAQEA"},
		{`token: ghp_abcdefghijklmnopqrstuvwxyz0123456789ABCD`, "ghp_abcdefghij"},
	}
	for _, c := range cases {
		out := r.String(c.in)
		if strings.Contains(out, c.mustNotContain) {
			t.Errorf("not redacted: %q -> %q", c.in, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("no mask in %q", out)
		}
	}
	// Ordinary log lines survive intact.
	keep := `panic: env PAYMENT_GW_URL required; pod checkout-api-7d9f8b6c4-x2k9p restarted 14 times`
	if got := r.String(keep); got != keep {
		t.Errorf("over-redacted: %q", got)
	}
	// Bob's plain user field survives while password is masked.
	if got := r.String(`{"password":"hunter22","user":"bob"}`); !strings.Contains(got, `"user":"bob"`) {
		t.Errorf("collateral damage: %q", got)
	}
}

func TestLooksLikeIdentifier(t *testing.T) {
	if !LooksLikeIdentifier("8f3a1c2b-4d5e-6f70-8192-a3b4c5d6e7f8") {
		t.Error("uuid not flagged")
	}
	if LooksLikeIdentifier("checkout-api") || LooksLikeIdentifier("payments") {
		t.Error("plain names flagged")
	}
}
