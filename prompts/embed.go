// Package prompts holds the two agent instructions as editable markdown,
// embedded into the binary so a deployment carries its own prompts.
package prompts

import _ "embed"

// Judge is the instruction for the verification agent.
//
//go:embed judge.md
var Judge string

// SRE is the instruction for the remediation agent.
//
//go:embed sre.md
var SRE string
