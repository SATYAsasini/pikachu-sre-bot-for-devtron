package agents

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/genai"
)

// The Messages API enforces two rules genai does not, and both cost us a live
// run to discover. Claude 4.6 and later rejected the whole investigation with
// "This model does not support assistant message prefill. The conversation
// must end with a user message", because an ADK loop hands the next iteration
// a history whose last entry is the previous iteration's own output.
func TestMessagesEndWithAUserTurn(t *testing.T) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: "investigate"}}},
		{Role: "model", Parts: []*genai.Part{{Text: "first pass"}}},
	}
	msgs, err := anthropicMessages(contents)
	if err != nil {
		t.Fatal(err)
	}
	last := msgs[len(msgs)-1]
	if last.Role != anthropic.MessageParamRoleUser {
		t.Fatalf("history ends on %s; Claude rejects that as prefill", last.Role)
	}
	if len(msgs) != 3 {
		t.Errorf("expected the assistant turn to be closed with one user turn, got %d messages", len(msgs))
	}
	// A history that already ends with the user is left alone.
	msgs, _ = anthropicMessages(append(contents, &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "go on"}}}))
	if n := len(msgs); n != 3 {
		t.Errorf("a well-formed history was padded: %d messages", n)
	}
}

// Tool results must ride in a user turn whatever role genai labelled them
// with, or the API rejects the tool_result block.
func TestToolResultsRideInAUserTurn(t *testing.T) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: "go"}}},
		{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "c1", Name: "k8s_describe", Args: map[string]any{"name": "p"}}}}},
		// ADK may label the response turn "model"; Anthropic requires user.
		{Role: "model", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "c1", Name: "k8s_describe", Response: map[string]any{"summary": "ok"}}}}},
	}
	msgs, err := anthropicMessages(contents)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected user, assistant, user; got %d turns", len(msgs))
	}
	if msgs[1].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("the tool call must be an assistant turn, got %s", msgs[1].Role)
	}
	if msgs[2].Role != anthropic.MessageParamRoleUser {
		t.Errorf("the tool result must be a user turn, got %s", msgs[2].Role)
	}
}

// A tool whose schema takes no arguments still has to present a valid schema,
// and an error result must be marked as one so the planner can react.
func TestToolSchemaAndErrorFlag(t *testing.T) {
	tools := anthropicTools([]*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
		{Name: "component_list", Description: "List components.", Parameters: nil},
		{Name: "k8s_describe", Description: "Describe.", Parameters: &genai.Schema{
			Type:       genai.TypeObject,
			Properties: map[string]*genai.Schema{"name": {Type: genai.TypeString, Description: "object name"}},
			Required:   []string{"name"},
		}},
	}}})
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if !responseIsError(map[string]any{"error": map[string]any{"code": "rbac_forbidden"}}) {
		t.Error("a tool result carrying an error must be flagged")
	}
	if responseIsError(map[string]any{"summary": "fine"}) {
		t.Error("a normal result must not be flagged as an error")
	}
}

// A run that dies because the Anthropic account is out of credit must not
// read as a bug in this service. The SDK renders that as a bare
// "400 Bad Request" and keeps the reason in the body.
func TestExplainAPIError(t *testing.T) {
	t.Parallel()

	apiErr := func(status int, body string) error {
		e := &anthropic.Error{StatusCode: status}
		_ = e.UnmarshalJSON([]byte(body))
		return e
	}
	const credit = `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API."}}`

	tests := []struct {
		name     string
		err      error
		contains []string
	}{
		{
			name:     "a spent balance says so and says where to fix it",
			err:      apiErr(400, credit),
			contains: []string{"credit balance is too low", "Nothing is wrong with this cluster"},
		},
		{
			name:     "another 400 is passed through verbatim",
			err:      apiErr(400, `{"error":{"message":"max_tokens: must be <= 64000"}}`),
			contains: []string{"max_tokens"},
		},
		{
			name:     "401 points at the key",
			err:      apiErr(401, `{"error":{"message":"invalid x-api-key"}}`),
			contains: []string{"invalid x-api-key", "ANTHROPIC_API_KEY"},
		},
		{
			name:     "429 says the run can be retried",
			err:      apiErr(429, `{"error":{"message":"rate limit exceeded"}}`),
			contains: []string{"rate limit exceeded", "retried"},
		},
		{
			name:     "a non-API error is returned unchanged",
			err:      errors.New("dial tcp: connection refused"),
			contains: []string{"connection refused"},
		},
		{
			name:     "an API error with an unparseable body falls back to the SDK text",
			err:      apiErr(500, `not json at all`),
			contains: []string{"500"},
		},
		{
			name:     "an API error with an empty message falls back to the SDK text",
			err:      apiErr(503, `{"error":{"message":"  "}}`),
			contains: []string{"503"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := explainAPIError(tt.err)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("want %q in %q", want, got)
				}
			}
		})
	}
}
