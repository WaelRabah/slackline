package inference

import (
	"context"
	"errors"
	"strings"
)

// Tool describes a function the model may call. Parameters is a JSON Schema object.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolHandler runs one tool call and returns the text the model reads next. The
// text is data from Slack and is treated as untrusted by the system prompt.
type ToolHandler func(ctx context.Context, name, arguments string) (string, error)

const (
	maxToolRounds = 8
	maxToolResult = 100000 // bytes returned to the model per call
)

// Assist answers a question with tool access. It loops: the model may call tools,
// the handler runs them, and the results go back until the model answers.
func Assist(ctx context.Context, settings Settings, system, question string, tools []Tool, handle ToolHandler) (string, error) {
	if !settings.Enabled {
		return "", errors.New("enable AI features in Settings first")
	}
	if err := settings.Validate(); err != nil {
		return "", err
	}
	question = strings.TrimSpace(question)
	if question == "" || len([]rune(question)) > maxQuestionRunes {
		return "", errors.New("question is empty or too long")
	}
	specs := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		specs = append(specs, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters}})
	}
	messages := []map[string]any{{"role": "system", "content": system}, {"role": "user", "content": question}}
	for round := 0; round < maxToolRounds; round++ {
		payload := map[string]any{"model": settings.ChatModel, "stream": false, "messages": messages, "tools": specs, "tool_choice": "auto", "max_tokens": 3000}
		choice, err := postChat(ctx, settings, payload)
		if err != nil {
			return "", err
		}
		calls := choice.Message.ToolCalls
		if len(calls) == 0 {
			answer := strings.TrimSpace(choice.Message.Content)
			if answer == "" {
				return "", errors.New("provider returned no answer")
			}
			if choice.Finish == "length" && answer != "" {
				return cutOff(answer), nil
			}
			if choice.Finish == "length" {
				return "", errors.New("provider truncated the answer; try a model with a larger output limit")
			}
			if len(answer) > 16000 {
				return "", errors.New("provider answer exceeded size limit")
			}
			return answer, nil
		}
		messages = append(messages, map[string]any{"role": "assistant", "content": choice.Message.Content, "tool_calls": calls})
		for _, call := range calls {
			result, err := handle(ctx, call.Function.Name, call.Function.Arguments)
			if err != nil {
				result = "Tool error: " + err.Error()
			}
			if len(result) > maxToolResult {
				result = strings.ToValidUTF8(result[:maxToolResult], "")
			}
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": call.ID, "content": result})
		}
		if err := ctx.Err(); err != nil {
			return "", errors.New("the assistant timed out; ask a narrower question")
		}
	}
	return "", errors.New("the assistant needed too many lookups; ask a narrower question")
}
