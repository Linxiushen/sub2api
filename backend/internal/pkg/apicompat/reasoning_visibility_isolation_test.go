package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// A truncated reasoning-only turn still carries useful status and usage, but
// neither a normal stop nor a token limit makes reasoning into answer text.
func TestReasoningIsolation_NonStreamingStatusAndUsage(t *testing.T) {
	for _, finish := range []string{"stop", "length"} {
		for _, alias := range []bool{false, true} {
			name := finish + "/reasoning_content"
			if alias {
				name = finish + "/reasoning"
			}
			t.Run(name, func(t *testing.T) {
				message := ChatMessage{Role: "assistant", Content: json.RawMessage(`null`)}
				if alias {
					message.Reasoning = "private planning only"
				} else {
					message.ReasoningContent = "private planning only"
					message.Reasoning = "ignored alias"
				}
				upstream := &ChatCompletionsResponse{
					Choices: []ChatChoice{{Message: message, FinishReason: finish}},
					Usage:   &ChatUsage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
				}

				responses := ChatCompletionsResponseToResponses(upstream, "test-model", nil, nil, false, nil)
				require.Len(t, responses.Output, 1)
				require.Equal(t, "reasoning", responses.Output[0].Type)
				require.Equal(t, "private planning only", responses.Output[0].Summary[0].Text)
				require.Empty(t, responses.Output[0].Content)
				require.NotNil(t, responses.Usage)
				require.Equal(t, 11, responses.Usage.InputTokens)
				require.Equal(t, 7, responses.Usage.OutputTokens)
				require.Equal(t, 18, responses.Usage.TotalTokens)

				anthropic := ChatCompletionsResponseToAnthropic(upstream, "test-model")
				require.Len(t, anthropic.Content, 1)
				require.Equal(t, "thinking", anthropic.Content[0].Type)
				require.Equal(t, "private planning only", anthropic.Content[0].Thinking)
				require.Empty(t, anthropic.Content[0].Text)
				require.Equal(t, 11, anthropic.Usage.InputTokens)
				require.Equal(t, 7, anthropic.Usage.OutputTokens)
				if finish == "length" {
					require.Equal(t, "incomplete", responses.Status)
					require.NotNil(t, responses.IncompleteDetails)
					require.Equal(t, "max_output_tokens", responses.IncompleteDetails.Reason)
					require.Equal(t, "max_tokens", AnthropicStopReasonString(anthropic.StopReason))
				} else {
					require.Equal(t, "completed", responses.Status)
					require.Nil(t, responses.IncompleteDetails)
					require.Equal(t, "end_turn", AnthropicStopReasonString(anthropic.StopReason))
				}
			})
		}
	}
}

func TestReasoningIsolation_RealAnswerAndLiteralTagsStayIntact(t *testing.T) {
	const answer = "Documented syntax: <analysis>example</analysis> and <think>example</think>."
	encoded, err := json.Marshal(answer)
	require.NoError(t, err)
	upstream := &ChatCompletionsResponse{Choices: []ChatChoice{{
		Message:      ChatMessage{Role: "assistant", Content: encoded, ReasoningContent: "private planning"},
		FinishReason: "stop",
	}}}
	responses := ChatCompletionsResponseToResponses(upstream, "test-model", nil, nil, false, nil)
	require.Len(t, responses.Output, 2)
	require.Equal(t, "reasoning", responses.Output[0].Type)
	require.Equal(t, "message", responses.Output[1].Type)
	require.Equal(t, answer, responses.Output[1].Content[0].Text)
	anthropic := ChatCompletionsResponseToAnthropic(upstream, "test-model")
	require.Len(t, anthropic.Content, 2)
	require.Equal(t, "thinking", anthropic.Content[0].Type)
	require.Equal(t, "text", anthropic.Content[1].Type)
	require.Equal(t, answer, anthropic.Content[1].Text)

	chunks := []string{
		`{"choices":[{"delta":{"reasoning_content":"private planning"}}]}`,
		`{"choices":[{"delta":{"content":"Documented syntax: <analysis>example</analysis> and <think>example</think>."},"finish_reason":"stop"}]}`,
	}
	var responsesText, anthropicText string
	for _, event := range collectStreamEvents(t, chunks) {
		if event.Type == "response.output_text.delta" {
			responsesText += event.Delta
		}
		if event.Type == "response.completed" {
			require.Equal(t, answer, event.Response.Output[1].Content[0].Text)
		}
	}
	for _, event := range collectAnthropicStreamEvents(t, chunks) {
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			anthropicText += event.Delta.Text
		}
	}
	require.Equal(t, answer, responsesText)
	require.Equal(t, answer, anthropicText)
}

func TestReasoningIsolation_NonStreamingToolCallPreserved(t *testing.T) {
	upstream := &ChatCompletionsResponse{Choices: []ChatChoice{{
		Message: ChatMessage{
			Role: "assistant", ReasoningContent: "need tool data",
			ToolCalls: []ChatToolCall{{ID: "call_123", Type: "function", Function: ChatFunctionCall{
				Name: "lookup", Arguments: `{"query":"hello"}`,
			}}},
		},
		FinishReason: "tool_calls",
	}}}
	responses := ChatCompletionsResponseToResponses(upstream, "test-model", nil, nil, false, nil)
	require.Len(t, responses.Output, 2)
	require.Equal(t, "reasoning", responses.Output[0].Type)
	require.Equal(t, "function_call", responses.Output[1].Type)
	require.Equal(t, "call_123", responses.Output[1].CallID)
	require.Equal(t, "lookup", responses.Output[1].Name)
	require.JSONEq(t, `{"query":"hello"}`, responses.Output[1].Arguments)
	anthropic := ChatCompletionsResponseToAnthropic(upstream, "test-model")
	require.Len(t, anthropic.Content, 2)
	require.Equal(t, "thinking", anthropic.Content[0].Type)
	require.Equal(t, "tool_use", anthropic.Content[1].Type)
	require.Equal(t, "call_123", anthropic.Content[1].ID)
	require.Equal(t, "lookup", anthropic.Content[1].Name)
	require.JSONEq(t, `{"query":"hello"}`, string(anthropic.Content[1].Input))
	require.Equal(t, "tool_use", AnthropicStopReasonString(anthropic.StopReason))
}

func TestReasoningIsolation_StreamingStatusUsageAndFinalizeIdempotence(t *testing.T) {
	for _, finish := range []string{"stop", "length"} {
		t.Run(finish, func(t *testing.T) {
			responsesState := NewChatCompletionsToResponsesStreamState("test-model")
			anthropicState := NewChatCompletionsToAnthropicStreamState("test-model")
			var responsesEvents []ResponsesStreamEvent
			var anthropicEvents []AnthropicStreamEvent
			chunks := []string{
				`{"choices":[{"delta":{"reasoning":"private "}}]}`,
				`{"choices":[{"delta":{"reasoning_content":"planning","reasoning":"ignored alias"}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"` + finish + `"}]}`,
				`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`,
			}
			for _, payload := range chunks {
				var chunk ChatCompletionsChunk
				require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
				responsesEvents = append(responsesEvents, ChatCompletionsChunkToResponsesEvents(&chunk, responsesState)...)
				anthropicEvents = append(anthropicEvents, ChatCompletionsChunkToAnthropicEvents(&chunk, anthropicState)...)
			}
			responsesEvents = append(responsesEvents, FinalizeChatCompletionsResponsesStream(responsesState)...)
			anthropicEvents = append(anthropicEvents, FinalizeChatCompletionsAnthropicStream(anthropicState)...)
			require.Empty(t, FinalizeChatCompletionsResponsesStream(responsesState))
			require.Empty(t, FinalizeChatCompletionsAnthropicStream(anthropicState))
			require.Empty(t, responsesState.Text.String())

			var sawCompleted, sawMessageDelta bool
			var thinking string
			for _, event := range responsesEvents {
				require.NotEqual(t, "response.output_text.delta", event.Type)
				require.NotEqual(t, "response.output_text.done", event.Type)
				if event.Type == "response.completed" {
					sawCompleted = true
					require.Len(t, event.Response.Output, 1)
					require.Equal(t, "reasoning", event.Response.Output[0].Type)
					require.Equal(t, "private planning", event.Response.Output[0].Summary[0].Text)
					require.NotNil(t, event.Response.Usage)
					require.Equal(t, 11, event.Response.Usage.InputTokens)
					require.Equal(t, 7, event.Response.Usage.OutputTokens)
					require.Equal(t, 18, event.Response.Usage.TotalTokens)
					if finish == "length" {
						require.Equal(t, "incomplete", event.Response.Status)
						require.Equal(t, "max_output_tokens", event.Response.IncompleteDetails.Reason)
					} else {
						require.Equal(t, "completed", event.Response.Status)
						require.Nil(t, event.Response.IncompleteDetails)
					}
				}
			}
			for _, event := range anthropicEvents {
				if event.ContentBlock != nil {
					require.Equal(t, "thinking", event.ContentBlock.Type)
				}
				if event.Delta != nil {
					require.NotEqual(t, "text_delta", event.Delta.Type)
					thinking += event.Delta.Thinking
				}
				if event.Type == "message_delta" {
					sawMessageDelta = true
					require.NotNil(t, event.Usage)
					require.Equal(t, 11, event.Usage.InputTokens)
					require.Equal(t, 7, event.Usage.OutputTokens)
					if finish == "length" {
						require.Equal(t, "max_tokens", event.Delta.StopReason)
					} else {
						require.Equal(t, "end_turn", event.Delta.StopReason)
					}
				}
			}
			require.True(t, sawCompleted)
			require.True(t, sawMessageDelta)
			require.Equal(t, "private planning", thinking)
		})
	}
}

func TestReasoningIsolation_TrulyEmptyTurnStillHasEmptyMessage(t *testing.T) {
	upstream := &ChatCompletionsResponse{Choices: []ChatChoice{{
		Message: ChatMessage{Role: "assistant"}, FinishReason: "stop",
	}}}
	responses := ChatCompletionsResponseToResponses(upstream, "test-model", nil, nil, false, nil)
	require.Len(t, responses.Output, 1)
	require.Equal(t, "message", responses.Output[0].Type)
	require.Empty(t, responses.Output[0].Content[0].Text)
	anthropic := ChatCompletionsResponseToAnthropic(upstream, "test-model")
	require.Len(t, anthropic.Content, 1)
	require.Equal(t, "text", anthropic.Content[0].Type)
	require.Empty(t, anthropic.Content[0].Text)
}
