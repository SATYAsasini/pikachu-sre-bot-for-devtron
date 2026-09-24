// Package prompts holds the agent instruction as editable markdown, embedded
// into the binary so a deployment carries its own prompt.
package prompts

import _ "embed"

// SRE is the instruction for the one agent this binary runs. It grades
// Devtron's first pass and writes remediation in a single pass; there was a
// separate judge prompt until the two agents were merged.
//
//go:embed sre.md
var SRE string
