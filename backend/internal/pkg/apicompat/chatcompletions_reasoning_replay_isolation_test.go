package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatReasoningReplay_FieldPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, primary, alias, want string
	}{
		{name: "reasoning_content", primary: "primary plan", want: "primary plan"},
		{name: "reasoning alias", alias: "alias plan", want: "alias plan"},
		{name: "primary wins", primary: "primary plan", alias: "unused alias", want: "primary plan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &ChatCompletionsRequest{Model: "deepseek-reasoner", Messages: []ChatMessage{
				{Role: "user", Content: json.RawMessage(`"Question"`)},
				{Role: "assistant", ReasoningContent: tc.primary, Reasoning: tc.alias, Content: json.RawMessage(`"Answer"`)},
			}}
			responses, err := ChatCompletionsToResponses(req)
			require.NoError(t, err)
			var items []ResponsesInputItem
			require.NoError(t, json.Unmarshal(responses.Input, &items))
			require.Len(t, items, 3)
			assert.Equal(t, "reasoning", items[1].Type)
			assert.Equal(t, []ResponsesSummary{{Type: "summary_text", Text: tc.want}}, items[1].Summary)
			assert.Empty(t, items[1].Content)
			var parts []ResponsesContentPart
			require.NoError(t, json.Unmarshal(items[2].Content, &parts))
			assert.Equal(t, []ResponsesContentPart{{Type: "output_text", Text: "Answer"}}, parts)

			replayed, err := ResponsesToChatCompletionsRequest(responses)
			require.NoError(t, err)
			require.Len(t, replayed.Messages, 2)
			assert.Equal(t, tc.want, replayed.Messages[1].ReasoningContent)
			assert.JSONEq(t, `"Answer"`, string(replayed.Messages[1].Content))
		})
	}
}

func TestChatReasoningReplay_ReasoningOnlyDoesNotCreateVisibleMessage(t *testing.T) {
	for _, content := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`""`)} {
		req := &ChatCompletionsRequest{Model: "deepseek-reasoner", Messages: []ChatMessage{
			{Role: "assistant", Content: content, ReasoningContent: "internal plan only"},
		}}
		responses, err := ChatCompletionsToResponses(req)
		require.NoError(t, err)
		var items []ResponsesInputItem
		require.NoError(t, json.Unmarshal(responses.Input, &items))
		require.Len(t, items, 1)
		assert.Equal(t, "reasoning", items[0].Type)
		assert.Empty(t, items[0].Content)
		assert.Empty(t, items[0].Role)
		assert.Equal(t, []ResponsesSummary{{Type: "summary_text", Text: "internal plan only"}}, items[0].Summary)
	}
}

func TestChatReasoningReplay_StructuredPartsStaySeparate(t *testing.T) {
	req := &ChatCompletionsRequest{Model: "reasoning-model", Messages: []ChatMessage{
		{Role: "assistant", ReasoningContent: "explicit plan", Content: json.RawMessage(`[
			{"type":"thinking","thinking":"structured thought"},
			{"type":"reasoning","text":"; structured reasoning"},
			{"type":"text","text":"Visible "},
			{"type":"output_text","text":"answer"}
		]`)},
	}}
	responses, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(responses.Input, &items))
	require.Len(t, items, 2)
	assert.Equal(t, []ResponsesSummary{{Type: "summary_text", Text: "explicit plan\nstructured thought; structured reasoning"}}, items[0].Summary)
	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[1].Content, &parts))
	assert.Equal(t, []ResponsesContentPart{{Type: "output_text", Text: "Visible answer"}}, parts)
}

func TestChatReasoningReplay_LiteralTagsRemainVisible(t *testing.T) {
	want := "Example: `<analysis>literal</analysis>` and `<thinking>literal</thinking>`\nDo not rewrite this code."
	encoded, err := json.Marshal(want)
	require.NoError(t, err)
	structured, err := json.Marshal([]ChatContentPart{{Type: "text", Text: want}})
	require.NoError(t, err)
	for _, content := range []json.RawMessage{encoded, structured} {
		req := &ChatCompletionsRequest{Model: "example-model", Messages: []ChatMessage{{Role: "assistant", Content: content}}}
		responses, err := ChatCompletionsToResponses(req)
		require.NoError(t, err)
		var items []ResponsesInputItem
		require.NoError(t, json.Unmarshal(responses.Input, &items))
		require.Len(t, items, 1)
		assert.Equal(t, "message", items[0].Type)
		assert.Empty(t, items[0].Summary)
		var parts []ResponsesContentPart
		require.NoError(t, json.Unmarshal(items[0].Content, &parts))
		require.Len(t, parts, 1)
		assert.Equal(t, want, parts[0].Text)
	}
}

func TestChatReasoningReplay_MultiRoundToolHistory(t *testing.T) {
	req := &ChatCompletionsRequest{Model: "deepseek-reasoner", Messages: []ChatMessage{
		{Role: "user", Content: json.RawMessage(`"Inspect then read the file"`)},
		{Role: "assistant", Content: json.RawMessage(`"Checking the directory."`), ReasoningContent: "first tool plan", ToolCalls: []ChatToolCall{{
			ID: "call_list_1", Type: "function", Function: ChatFunctionCall{Name: "list_files", Arguments: `{}`},
		}}},
		{Role: "tool", ToolCallID: "call_list_1", Content: json.RawMessage(`"README.md"`)},
		{Role: "assistant", ReasoningContent: "second tool plan", ToolCalls: []ChatToolCall{{
			ID: "call_read_2", Type: "function", Function: ChatFunctionCall{Name: "read_file", Arguments: `{"path":"README.md"}`},
		}}},
		{Role: "tool", ToolCallID: "call_read_2", Content: json.RawMessage(`"Project details"`)},
		{Role: "assistant", ReasoningContent: "final plan", Content: json.RawMessage(`"The README describes the project."`)},
		{Role: "user", Content: json.RawMessage(`"Continue"`)},
	}}

	// Repeat the conversion twice to catch contamination accumulating during
	// successive tool rounds, rather than checking only one isolated message.
	for round := 0; round < 2; round++ {
		responses, err := ChatCompletionsToResponses(req)
		require.NoError(t, err)
		var items []ResponsesInputItem
		require.NoError(t, json.Unmarshal(responses.Input, &items))
		for _, item := range items {
			if item.Type != "message" || item.Role != "assistant" {
				continue
			}
			var parts []ResponsesContentPart
			require.NoError(t, json.Unmarshal(item.Content, &parts))
			for _, part := range parts {
				assert.NotContains(t, part.Text, "tool plan")
				assert.NotContains(t, part.Text, "final plan")
				assert.NotContains(t, part.Text, "<thinking>")
			}
		}

		req, err = ResponsesToChatCompletionsRequest(responses)
		require.NoError(t, err)
		require.Len(t, req.Messages, 7)
		assert.Equal(t, "first tool plan", req.Messages[1].ReasoningContent)
		assert.JSONEq(t, `"Checking the directory."`, string(req.Messages[1].Content))
		require.Len(t, req.Messages[1].ToolCalls, 1)
		assert.Equal(t, "call_list_1", req.Messages[1].ToolCalls[0].ID)
		assert.Equal(t, "call_list_1", req.Messages[2].ToolCallID)
		assert.JSONEq(t, `"README.md"`, string(req.Messages[2].Content))
		assert.Equal(t, "second tool plan", req.Messages[3].ReasoningContent)
		require.Len(t, req.Messages[3].ToolCalls, 1)
		assert.Equal(t, "call_read_2", req.Messages[3].ToolCalls[0].ID)
		assert.Equal(t, "call_read_2", req.Messages[4].ToolCallID)
		assert.JSONEq(t, `"Project details"`, string(req.Messages[4].Content))
		assert.Equal(t, "final plan", req.Messages[5].ReasoningContent)
		assert.JSONEq(t, `"The README describes the project."`, string(req.Messages[5].Content))
	}
}

func TestChatReasoningReplay_AnthropicDoesNotTreatPlainSummaryAsVisibleOrSignedThinking(t *testing.T) {
	req := &ChatCompletionsRequest{Model: "claude-opus-5-5", Messages: []ChatMessage{
		{Role: "user", Content: json.RawMessage(`"Read the file"`)},
		{Role: "assistant", Content: json.RawMessage(`"Reading the file."`), ReasoningContent: "PRIVATE_UNSIGNABLE_PLAN", ToolCalls: []ChatToolCall{{
			ID: "call_file_1", Type: "function", Function: ChatFunctionCall{Name: "read_file", Arguments: `{"path":"README.md"}`},
		}}},
		{Role: "tool", ToolCallID: "call_file_1", Content: json.RawMessage(`"Project details"`)},
	}}
	responses, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	anthropic, err := ResponsesToAnthropicRequest(responses)
	require.NoError(t, err)
	var toolUseID, toolResultID string
	var visible []string
	for _, message := range anthropic.Messages {
		assert.NotContains(t, string(message.Content), "PRIVATE_UNSIGNABLE_PLAN")
		var text string
		if err := json.Unmarshal(message.Content, &text); err == nil {
			visible = append(visible, text)
			continue
		}
		var blocks []AnthropicContentBlock
		require.NoError(t, json.Unmarshal(message.Content, &blocks))
		for _, block := range blocks {
			assert.NotEqual(t, "thinking", block.Type, "a plaintext summary cannot supply a provider signature")
			switch block.Type {
			case "text":
				visible = append(visible, block.Text)
			case "tool_use":
				toolUseID = block.ID
				assert.JSONEq(t, `{"path":"README.md"}`, string(block.Input))
			case "tool_result":
				toolResultID = block.ToolUseID
				assert.JSONEq(t, `"Project details"`, string(block.Content))
			}
		}
	}
	assert.Contains(t, visible, "Reading the file.")
	assert.NotEmpty(t, toolUseID)
	assert.Equal(t, toolUseID, toolResultID)
}

func TestChatReasoningReplay_SummarySurvivesInputItemWireRoundTrip(t *testing.T) {
	want := ResponsesInputItem{Type: "reasoning", Summary: []ResponsesSummary{{Type: "summary_text", Text: "reasoning only"}}}
	wire, err := json.Marshal(want)
	require.NoError(t, err)
	assert.NotContains(t, string(wire), `"content"`)
	var got ResponsesInputItem
	require.NoError(t, json.Unmarshal(wire, &got))
	assert.Equal(t, want, got)
}
