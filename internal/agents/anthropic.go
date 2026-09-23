package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// AnthropicLLM adapts Claude to the ADK model interface. ADK ships adapters
// for Gemini only, but model.LLM is a two-method interface over genai types,
// so speaking Claude is a matter of translating the request and response.
//
// The translation is: genai roles user/model become Anthropic user/assistant,
// text parts become text blocks, function calls become tool_use blocks,
// function responses become tool_result blocks, and the genai tool schemas
// become Anthropic tool definitions.
type AnthropicLLM struct {
	client    anthropic.Client
	modelID   string
	maxTokens int64
	// Thinking turns on adaptive thinking, which Claude 4.6 and later use to
	// decide how much to reason per request. Worth it for the analyst and
	// critic; wasteful for a one-line triage classification.
	Thinking bool
	// Effort is low, medium, high, xhigh or max. Empty leaves the default.
	Effort string
}

// AnthropicOptions configure the adapter.
type AnthropicOptions struct {
	APIKey    string
	BaseURL   string
	ModelID   string
	MaxTokens int64
	Thinking  bool
	Effort    string
}

// NewAnthropic builds a Claude-backed model. The API key comes from the
// options or from ANTHROPIC_API_KEY.
func NewAnthropic(o AnthropicOptions) (*AnthropicLLM, error) {
	key := o.APIKey
	if key == "" {
		key = os.Getenv("SRE_ANTHROPIC_API_KEY")
	}
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("no Anthropic credentials: set SRE_ANTHROPIC_API_KEY or ANTHROPIC_API_KEY")
	}
	if o.ModelID == "" {
		o.ModelID = "claude-opus-5"
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = 16000
	}
	opts := []option.RequestOption{option.WithAPIKey(key)}
	if o.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(o.BaseURL))
	}
	return &AnthropicLLM{
		client: anthropic.NewClient(opts...), modelID: o.ModelID, maxTokens: o.MaxTokens,
		Thinking: o.Thinking, Effort: o.Effort,
	}, nil
}

// Name implements model.LLM.
func (a *AnthropicLLM) Name() string { return a.modelID }

// GenerateContent implements model.LLM. The ADK runner consumes one response
// per call for non-streaming use, which is what the investigation workflows
// need: each agent step is a discrete request whose result is recorded in the
// evidence ledger.
func (a *AnthropicLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		params, err := a.buildParams(req)
		if err != nil {
			yield(nil, err)
			return
		}
		msg, err := a.client.Messages.New(ctx, *params)
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: %s", explainAPIError(err)))
			return
		}
		yield(toLLMResponse(msg), nil)
	}
}

func (a *AnthropicLLM) buildParams(req *model.LLMRequest) (*anthropic.MessageNewParams, error) {
	id := a.modelID
	if req.Model != "" && strings.HasPrefix(req.Model, "claude") {
		id = req.Model
	}
	params := anthropic.MessageNewParams{Model: anthropic.Model(id), MaxTokens: a.maxTokens}

	if a.Thinking {
		adaptive := anthropic.ThinkingConfigAdaptiveParam{}
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
	}

	if cfg := req.Config; cfg != nil {
		if cfg.SystemInstruction != nil {
			var sys []anthropic.TextBlockParam
			for _, p := range cfg.SystemInstruction.Parts {
				if p != nil && p.Text != "" {
					sys = append(sys, anthropic.TextBlockParam{Text: p.Text})
				}
			}
			if len(sys) > 0 {
				// Cache the instruction: the global contract, specialist
				// prompt and playbook are identical across the steps of a run.
				sys[len(sys)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
				params.System = sys
			}
		}
		if cfg.MaxOutputTokens > 0 {
			params.MaxTokens = int64(cfg.MaxOutputTokens)
		}
		if tools := anthropicTools(cfg.Tools); len(tools) > 0 {
			params.Tools = tools
		}
	}

	msgs, err := anthropicMessages(req.Contents)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("anthropic: request has no messages")
	}
	params.Messages = msgs
	return &params, nil
}

// anthropicMessages converts genai contents, merging consecutive turns of the
// same role because Anthropic requires strictly alternating roles.
//
// Two rules the Messages API enforces and genai does not:
//   - tool results must be carried by a USER turn, whatever role the caller
//     labelled them with;
//   - the conversation must END with a user turn. Continuing from a trailing
//     assistant message is prefill, which Claude 4.6 and later reject with
//     "This model does not support assistant message prefill". ADK hits this
//     on every loop iteration, where the previous turn's output is the last
//     thing in the history.
func anthropicMessages(contents []*genai.Content) ([]anthropic.MessageParam, error) {
	var out []anthropic.MessageParam
	for _, c := range contents {
		if c == nil || len(c.Parts) == 0 {
			continue
		}
		role := anthropic.MessageParamRoleAssistant
		if strings.EqualFold(c.Role, "user") || c.Role == "" || hasFunctionResponse(c.Parts) {
			role = anthropic.MessageParamRoleUser
		}
		blocks, err := anthropicBlocks(c.Parts)
		if err != nil {
			return nil, err
		}
		if len(blocks) == 0 {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			continue
		}
		out = append(out, anthropic.MessageParam{Role: role, Content: blocks})
	}
	if n := len(out); n > 0 && out[n-1].Role == anthropic.MessageParamRoleAssistant {
		out = append(out, anthropic.MessageParam{
			Role:    anthropic.MessageParamRoleUser,
			Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(continueTurn)},
		})
	}
	return out, nil
}

// continueTurn closes a history that would otherwise end on an assistant
// message. It carries no instruction of its own: the step's real instruction
// is in the system prompt, and adding guidance here would quietly compete
// with it.
const continueTurn = "Continue."

func hasFunctionResponse(parts []*genai.Part) bool {
	for _, p := range parts {
		if p != nil && p.FunctionResponse != nil {
			return true
		}
	}
	return false
}

func anthropicBlocks(parts []*genai.Part) ([]anthropic.ContentBlockParamUnion, error) {
	var out []anthropic.ContentBlockParamUnion
	for _, p := range parts {
		if p == nil {
			continue
		}
		switch {
		case p.FunctionCall != nil:
			id := p.FunctionCall.ID
			if id == "" {
				id = "call_" + p.FunctionCall.Name
			}
			args := p.FunctionCall.Args
			if args == nil {
				args = map[string]any{}
			}
			out = append(out, anthropic.NewToolUseBlock(id, args, p.FunctionCall.Name))

		case p.FunctionResponse != nil:
			id := p.FunctionResponse.ID
			if id == "" {
				id = "call_" + p.FunctionResponse.Name
			}
			body, err := json.Marshal(p.FunctionResponse.Response)
			if err != nil {
				body = []byte(fmt.Sprintf("%v", p.FunctionResponse.Response))
			}
			// A tool that reported an error is still a result, not a failure
			// of the turn: the planner is told so it can choose differently.
			isErr := responseIsError(p.FunctionResponse.Response)
			out = append(out, anthropic.NewToolResultBlock(id, string(body), isErr))

		case p.Text != "":
			out = append(out, anthropic.NewTextBlock(p.Text))
		}
	}
	return out, nil
}

func responseIsError(resp map[string]any) bool {
	if resp == nil {
		return false
	}
	if e, ok := resp["error"]; ok && e != nil {
		return true
	}
	return false
}

// anthropicTools converts genai function declarations to Anthropic tools.
func anthropicTools(in []*genai.Tool) []anthropic.ToolUnionParam {
	var out []anthropic.ToolUnionParam
	for _, t := range in {
		if t == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil || fd.Name == "" {
				continue
			}
			schema := jsonSchemaFrom(fd.Parameters)
			props, _ := schema["properties"].(map[string]any)
			if props == nil {
				props = map[string]any{}
			}
			tool := anthropic.ToolParam{
				Name:        fd.Name,
				Description: anthropic.String(fd.Description),
				InputSchema: anthropic.ToolInputSchemaParam{Properties: props},
			}
			if req, ok := schema["required"].([]any); ok && len(req) > 0 {
				tool.InputSchema.Required = toStrings(req)
			}
			out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
		}
	}
	return out
}

func toStrings(in []any) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// jsonSchemaFrom renders a genai schema as a plain JSON Schema document.
func jsonSchemaFrom(s *genai.Schema) map[string]any {
	if s == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	out := map[string]any{}
	if s.Type != "" {
		out["type"] = strings.ToLower(string(s.Type))
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Items != nil {
		out["items"] = jsonSchemaFrom(s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = jsonSchemaFrom(v)
		}
		out["properties"] = props
	}
	if len(s.Required) > 0 {
		req := make([]any, 0, len(s.Required))
		for _, r := range s.Required {
			req = append(req, r)
		}
		out["required"] = req
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	return out
}

// toLLMResponse converts an Anthropic message back to the ADK shape.
func toLLMResponse(msg *anthropic.Message) *model.LLMResponse {
	content := &genai.Content{Role: string(genai.RoleModel)}
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			if b.Text != "" {
				content.Parts = append(content.Parts, &genai.Part{Text: b.Text})
			}
		case anthropic.ToolUseBlock:
			args := map[string]any{}
			if len(b.Input) > 0 {
				// Tool inputs must be parsed, never string-matched: escaping
				// varies between models.
				_ = json.Unmarshal(b.Input, &args)
			}
			content.Parts = append(content.Parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{ID: b.ID, Name: b.Name, Args: args},
			})
		}
	}
	if len(content.Parts) == 0 {
		content.Parts = append(content.Parts, &genai.Part{Text: ""})
	}

	resp := &model.LLMResponse{
		Content:      content,
		ModelVersion: string(msg.Model),
		TurnComplete: true,
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(msg.Usage.InputTokens),
			CandidatesTokenCount: int32(msg.Usage.OutputTokens),
			TotalTokenCount:      int32(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		},
	}
	switch msg.StopReason {
	case anthropic.StopReasonMaxTokens:
		resp.FinishReason = genai.FinishReasonMaxTokens
	case anthropic.StopReasonRefusal:
		// A policy decline is a terminal outcome for this step, not a crash:
		// the run records it and finishes with what it has.
		resp.FinishReason = genai.FinishReasonSafety
		resp.ErrorCode = "refusal"
		resp.ErrorMessage = "the model declined this request"
		if msg.StopDetails.Category != "" {
			resp.ErrorMessage += " (" + string(msg.StopDetails.Category) + ")"
		}
	default:
		resp.FinishReason = genai.FinishReasonStop
	}
	return resp
}

// explainAPIError turns the SDK's error into something a run page can act on.
//
// The SDK renders a refusal as `POST ".../v1/messages": 400 Bad Request` and
// keeps the actual reason — an exhausted credit balance, a model this key
// cannot reach, an oversized request — only in the response body, reachable
// through RawJSON. A run that died because the account is out of credit then
// reads as a bug in this service, and the first hour of debugging goes to the
// wrong place.
func explainAPIError(err error) string {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return err.Error()
	}

	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	detail := ""
	if raw := apiErr.RawJSON(); raw != "" {
		if json.Unmarshal([]byte(raw), &body) == nil {
			detail = strings.TrimSpace(body.Error.Message)
		}
	}
	if detail == "" {
		// Not err.Error(): the SDK's own formatter dereferences Request and
		// Response unconditionally, so it panics on any Error that did not
		// come from a completed round trip.
		msg := fmt.Sprintf("HTTP %d from the Messages API", apiErr.StatusCode)
		if apiErr.RequestID != "" {
			msg += " (request " + apiErr.RequestID + ")"
		}
		return msg
	}

	switch apiErr.StatusCode {
	case http.StatusBadRequest, http.StatusPaymentRequired:
		// 400 is where Anthropic reports a spent balance, so say what to do
		// about it rather than leaving "bad request" to be read as our bug.
		if strings.Contains(strings.ToLower(detail), "credit balance") {
			// Anthropic's own text already says where to go, so only add what
			// it does not: that the cluster and the run are not at fault.
			return detail + " Nothing is wrong with this cluster or with the run."
		}
		return detail
	case http.StatusUnauthorized, http.StatusForbidden:
		return detail + " — check ANTHROPIC_API_KEY in .env."
	case http.StatusTooManyRequests:
		return detail + " — rate limited; the run can be retried unchanged."
	default:
		return detail
	}
}
