package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardAsChatCompletions_ConvertedReasoningNativeOutboundGate(t *testing.T) {
	for _, tc := range []struct {
		name          string
		accountType   string
		baseURL       string
		wantReasoning bool
		wantHost      string
	}{
		{name: "Codex OAuth", accountType: AccountTypeOAuth, wantHost: "chatgpt.com"},
		{name: "Codex setup token", accountType: AccountTypeSetupToken, wantHost: "chatgpt.com"},
		{name: "official API key default", accountType: AccountTypeAPIKey, wantHost: "api.openai.com"},
		{name: "official API key explicit v1", accountType: AccountTypeAPIKey, baseURL: "https://api.openai.com/v1", wantHost: "api.openai.com"},
		{name: "third party Responses", accountType: AccountTypeAPIKey, baseURL: "https://compatible.example/v1", wantReasoning: true, wantHost: "compatible.example"},
		{name: "OpenAI-like third-party hostname", accountType: AccountTypeAPIKey, baseURL: "https://api.openai.com.compatible.example/v1", wantReasoning: true, wantHost: "api.openai.com.compatible.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := convertedReasoningTestAccount(tc.accountType, tc.baseURL)
			body := []byte(`{"model":"gpt-5.1","messages":[
				{"role":"user","content":"Look up the example"},
				{"role":"assistant","reasoning_content":"PRIVATE_REPLAY_PLAN","content":[{"type":"thinking","thinking":"PRIVATE_STRUCTURED_PLAN"},{"type":"text","text":"Keep <analysis>literal</analysis> in this example."}],"tool_calls":[{"id":"call_lookup_1","type":"function","function":{"name":"lookup","arguments":"{\"key\":\"value\"}"}}]},
				{"role":"tool","tool_call_id":"call_lookup_1","content":"lookup result"},
				{"role":"user","content":"Continue"}
			],"stream":false}`)
			outbound, upstreamRequest := captureConvertedReasoningServiceRequest(t, account, body)
			require.Equal(t, tc.wantHost, upstreamRequest.URL.Hostname())
			require.True(t, strings.HasSuffix(upstreamRequest.URL.Path, "/responses"))
			var items []apicompat.ResponsesInputItem
			require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(outbound, "input").Raw), &items))
			var reasoningCount int
			var visibleAssistant string
			var callID, outputCallID, output string
			for _, item := range items {
				switch item.Type {
				case "reasoning":
					reasoningCount++
					require.Equal(t, []apicompat.ResponsesSummary{{Type: "summary_text", Text: "PRIVATE_REPLAY_PLAN\nPRIVATE_STRUCTURED_PLAN"}}, item.Summary)
				case "function_call":
					callID = item.CallID
					require.Equal(t, "lookup", item.Name)
					require.JSONEq(t, `{"key":"value"}`, item.Arguments)
				case "function_call_output":
					outputCallID, output = item.CallID, item.Output
				case "message":
					if item.Role == "assistant" {
						var content []apicompat.ResponsesContentPart
						require.NoError(t, json.Unmarshal(item.Content, &content))
						for _, part := range content {
							visibleAssistant += part.Text
						}
					}
				}
			}
			if tc.wantReasoning {
				require.Equal(t, 1, reasoningCount)
			} else {
				require.Zero(t, reasoningCount)
				require.NotContains(t, string(outbound), "PRIVATE_REPLAY_PLAN")
				require.NotContains(t, string(outbound), "PRIVATE_STRUCTURED_PLAN")
			}
			require.Equal(t, "Keep <analysis>literal</analysis> in this example.", visibleAssistant)
			wantCallID := "call_lookup_1"
			if account.UsesOpenAICodexProtocol() {
				// The existing Codex transform normalizes both sides of a
				// tool pair to its native fc_ prefix.
				wantCallID = "fc_lookup_1"
			}
			require.Equal(t, wantCallID, callID)
			require.Equal(t, callID, outputCallID)
			require.Equal(t, "lookup result", output)
		})
	}
}

func TestForwardAsChatCompletions_ConvertedReasoningGateDoesNotRewriteNativeResponsesInput(t *testing.T) {
	account := convertedReasoningTestAccount(AccountTypeAPIKey, "https://api.openai.com")
	body := []byte(`{"model":"gpt-5.1","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue"}]},{"type":"reasoning","id":"rs_native_stored","summary":[{"type":"summary_text","text":"native client stored summary"}]},{"type":"reasoning","id":"rs_native_encrypted","summary":[{"type":"summary_text","text":"native client encrypted summary"}],"encrypted_content":"native-encrypted-payload"}],"stream":false}`)
	outbound, _ := captureConvertedReasoningServiceRequest(t, account, body)
	require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(outbound, "input").Raw)
}

func TestOmitSyntheticChatReasoningForNativeResponses_PreservesEncryptedAndToolPayloads(t *testing.T) {
	req := &apicompat.ResponsesRequest{Input: json.RawMessage(`[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"synthetic plaintext"}]},
		{"type":"reasoning","id":"rs_native","encrypted_content":"native-payload","summary":[{"type":"summary_text","text":"native summary"}],"opaque":{"keep":true}},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"literal <thinking>example</thinking>"}]},
		{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_1","output":{"result":"untouched"}}
	]`)}
	var before []json.RawMessage
	require.NoError(t, json.Unmarshal(req.Input, &before))
	require.NoError(t, omitSyntheticChatReasoningForNativeResponses(req))
	var after []json.RawMessage
	require.NoError(t, json.Unmarshal(req.Input, &after))
	require.Len(t, after, 4)
	for i, item := range after {
		require.JSONEq(t, string(before[i+1]), string(item))
	}
	stableInput := append([]byte(nil), req.Input...)
	require.NoError(t, omitSyntheticChatReasoningForNativeResponses(req))
	require.Equal(t, stableInput, []byte(req.Input), "normalization must be idempotent")
}

func convertedReasoningTestAccount(accountType, baseURL string) *Account {
	credentials := map[string]any{"api_key": "fixture-key", "access_token": "fixture-token", "chatgpt_account_id": "fixture-account"}
	if baseURL != "" {
		credentials["base_url"] = baseURL
	}
	return &Account{ID: 201, Platform: PlatformOpenAI, Type: accountType, Concurrency: 1,
		Credentials: credentials, Extra: map[string]any{"openai_responses_supported": true}}
}

func captureConvertedReasoningServiceRequest(t *testing.T, account *Account, body []byte) ([]byte, *http.Request) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	responseBody := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_test","model":"gpt-5.1","status":"in_progress","output":[]}}`, "",
		`data: {"type":"response.output_text.delta","delta":"ok"}`, "",
		`data: {"type":"response.completed","response":{"id":"resp_test","model":"gpt-5.1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`, "",
		`data: [DONE]`, "",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
	svc := &OpenAIGatewayService{cfg: structuredInputRetryTestConfig(), httpUpstream: upstream}
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "ok", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
	return upstream.lastBody, upstream.lastReq
}
