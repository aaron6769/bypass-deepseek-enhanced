package deepseek

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/iocgo/sdk/env"
	"github.com/spf13/viper"
)

func TestWaitResponseKeepsFirstFragmentAfterTitleEvent(t *testing.T) {
	previousEnv := env.Env
	env.Env = &env.Environment{Viper: viper.New()}
	defer func() { env.Env = previousEnv }()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	stream := strings.Join([]string{
		"event: title",
		`data: {"p":"response/fragments/-1/content","v":"LOC"}`,
		`data: {"p":"response/fragments/-1/content","v":"AL_BYPASS_OK"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
		"",
	}, "\n")
	upstream := &http.Response{
		Body:   io.NopCloser(strings.NewReader(stream)),
		Header: make(http.Header),
	}

	if got := waitResponse(ctx, upstream, false); got != "LOCAL_BYPASS_OK" {
		t.Fatalf("waitResponse() = %q, want %q", got, "LOCAL_BYPASS_OK")
	}
}

func TestWaitResponseKeepsLongEventLine(t *testing.T) {
	previousEnv := env.Env
	env.Env = &env.Environment{Viper: viper.New()}
	defer func() { env.Env = previousEnv }()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	want := strings.Repeat("x", 70<<10)
	payload, err := json.Marshal(map[string]interface{}{
		"p": "response/fragments/-1/content",
		"v": want,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := strings.Join([]string{
		"data: " + string(payload),
		`data: {"p":"response/status","v":"FINISHED"}`,
		"",
	}, "\n")
	upstream := &http.Response{
		Body:   io.NopCloser(strings.NewReader(stream)),
		Header: make(http.Header),
	}

	if got := waitResponse(ctx, upstream, false); got != want {
		t.Fatalf("waitResponse() returned %d bytes, want %d", len(got), len(want))
	}
}

func TestWaitResponseKeepsContentFromAppendedFragmentObject(t *testing.T) {
	previousEnv := env.Env
	env.Env = &env.Environment{Viper: viper.New()}
	defer func() { env.Env = previousEnv }()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	stream := strings.Join([]string{
		"event: message",
		`data: {"p":"response/fragments/-1","o":"APPEND","v":{"type":"TEXT","content":"BOT"}}`,
		`data: {"p":"response/fragments/-1/content","o":"APPEND","v":"_EXPERT_OK"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
		"",
	}, "\n")
	upstream := &http.Response{
		Body:   io.NopCloser(strings.NewReader(stream)),
		Header: make(http.Header),
	}

	if got := waitResponse(ctx, upstream, false); got != "BOT_EXPERT_OK" {
		t.Fatalf("waitResponse() = %q, want %q", got, "BOT_EXPERT_OK")
	}
}

func TestWaitResponseParsesSnapshotAndInheritedContentPath(t *testing.T) {
	previousEnv := env.Env
	env.Env = &env.Environment{Viper: viper.New()}
	defer func() { env.Env = previousEnv }()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	stream := strings.Join([]string{
		"event: update_session",
		`data: {"v":{"response":{"fragments":[{"content":"LOC","type":"RESPONSE"}]}}}`,
		`data: {"p":"response/fragments/-1/content","o":"APPEND","v":"AL"}`,
		`data: {"v":"_B"}`,
		`data: {"v":"YP"}`,
		`data: {"v":"ASS"}`,
		`data: {"v":"_OK"}`,
		`data: {"p":"response/status","o":"SET","v":"FINISHED"}`,
		"",
	}, "\n")
	upstream := &http.Response{
		Body:   io.NopCloser(strings.NewReader(stream)),
		Header: make(http.Header),
	}

	if got := waitResponse(ctx, upstream, false); got != "LOCAL_BYPASS_OK" {
		t.Fatalf("waitResponse() = %q, want %q", got, "LOCAL_BYPASS_OK")
	}
}

func TestWaitResponseParsesBatchFragmentBeforeContentPath(t *testing.T) {
	previousEnv := env.Env
	env.Env = &env.Environment{Viper: viper.New()}
	defer func() { env.Env = previousEnv }()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	stream := strings.Join([]string{
		"event: update_session",
		`data: {"p":"response","o":"BATCH","v":[{"o":"APPEND","p":"fragments","v":[{"content":"B","id":3,"type":"RESPONSE"}]},{"o":"SET","p":"has_pending_fragment","v":false}]}`,
		`data: {"p":"response/fragments/-1/content","o":"APPEND","v":"OT"}`,
		`data: {"v":"_SEARCH_OK"}`,
		`data: {"p":"response/status","o":"SET","v":"FINISHED"}`,
		"",
	}, "\n")
	upstream := &http.Response{
		Body:   io.NopCloser(strings.NewReader(stream)),
		Header: make(http.Header),
	}

	if got := waitResponse(ctx, upstream, false); got != "BOT_SEARCH_OK" {
		t.Fatalf("waitResponse() = %q, want %q", got, "BOT_SEARCH_OK")
	}
}

func TestWaitResponseSeparatesThinkingFragmentsNonStream(t *testing.T) {
	previousEnv := env.Env
	env.Env = &env.Environment{Viper: viper.New()}
	defer func() { env.Env = previousEnv }()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	upstream := deepSeekThinkingTestResponse()

	if got := waitResponse(ctx, upstream, false); got != "FINAL_4" {
		t.Fatalf("waitResponse() content = %q, want %q", got, "FINAL_4")
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(payload.Choices))
	}
	if got := payload.Choices[0].Message.Content; got != "FINAL_4" {
		t.Fatalf("message.content = %q, want %q", got, "FINAL_4")
	}
	if got := payload.Choices[0].Message.ReasoningContent; got != "我们思考。" {
		t.Fatalf("message.reasoning_content = %q, want %q", got, "我们思考。")
	}
}

func TestWaitResponseSeparatesThinkingFragmentsStream(t *testing.T) {
	previousEnv := env.Env
	env.Env = &env.Environment{Viper: viper.New()}
	defer func() { env.Env = previousEnv }()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	upstream := deepSeekThinkingTestResponse()

	if got := waitResponse(ctx, upstream, true); got != "FINAL_4" {
		t.Fatalf("waitResponse() content = %q, want %q", got, "FINAL_4")
	}
	content, reasoning := deepSeekTestStreamText(t, recorder.Body.String())
	if content != "FINAL_4" {
		t.Fatalf("stream content = %q, want %q", content, "FINAL_4")
	}
	if reasoning != "我们思考。" {
		t.Fatalf("stream reasoning_content = %q, want %q", reasoning, "我们思考。")
	}
}

func deepSeekThinkingTestResponse() *http.Response {
	stream := strings.Join([]string{
		"event: update_session",
		`data: {"v":{"response":{"fragments":[{"content":"我们","stage_id":1,"type":"THINK"}]}}}`,
		`data: {"p":"response/fragments/-1/content","o":"APPEND","v":"思考"}`,
		`data: {"v":"。"}`,
		`data: {"p":"response/fragments/-1/elapsed_secs","o":"SET","v":0.5}`,
		`data: {"p":"response/fragments","o":"APPEND","v":[{"content":"F","stage_id":1,"type":"RESPONSE"}]}`,
		`data: {"p":"response/fragments/-1/content","o":"APPEND","v":"INAL"}`,
		`data: {"v":"_4"}`,
		`data: {"p":"response/status","o":"SET","v":"FINISHED"}`,
		"",
	}, "\n")
	return &http.Response{
		Body:   io.NopCloser(strings.NewReader(stream)),
		Header: make(http.Header),
	}
}

func deepSeekTestStreamText(t *testing.T, stream string) (content, reasoning string) {
	t.Helper()
	for _, line := range strings.Split(stream, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if line == "" || line == "[DONE]" {
			continue
		}
		var payload struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil || len(payload.Choices) == 0 {
			continue
		}
		content += payload.Choices[0].Delta.Content
		reasoning += payload.Choices[0].Delta.ReasoningContent
	}
	return content, reasoning
}
