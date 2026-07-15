package deepseek

import (
	"bufio"
	"bytes"
	"chatgpt-adapter/core/gin/model"
	"encoding/json"
	"fmt"
	"github.com/iocgo/sdk/env"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"chatgpt-adapter/core/common"
	"chatgpt-adapter/core/common/vars"
	"chatgpt-adapter/core/gin/response"
	"chatgpt-adapter/core/logger"
	"github.com/gin-gonic/gin"
)

const (
	ginTokens                 = "__tokens__"
	maxDeepSeekEventLineBytes = 8 << 20
)

func waitMessage(r *http.Response, cancel func(str string) bool) (content string, err error) {
	defer r.Body.Close()
	reader := bufio.NewReader(r.Body)
	var dataBytes []byte
	for {
		dataBytes, err = readDeepSeekEventLine(reader)
		if err == io.EOF {
			break
		}

		if err != nil {
			return
		}

		var res model.Response
		if bytes.HasPrefix(dataBytes, []byte("data: ")) {
			dataBytes = dataBytes[6:]
		}
		if len(dataBytes) == 0 {
			continue
		}

		err = json.Unmarshal(dataBytes, &res)
		if err != nil {
			logger.Warn(err)
			continue
		}

		if len(res.Choices) == 0 {
			continue
		}

		if res.Choices[0].FinishReason != nil && *res.Choices[0].FinishReason == "stop" {
			break
		}

		delta := res.Choices[0].Delta
		if delta.Type == "thinking" {
			continue
		}

		raw := delta.Content
		logger.Debug("----- raw -----")
		logger.Debug(raw)
		content += raw
		if cancel != nil && cancel(content) {
			return content, nil
		}
	}
	return
}

func waitResponse(ctx *gin.Context, r *http.Response, sse bool) (content string) {
	created := time.Now().Unix()
	logger.Infof("waitResponse ...")
	tokens := ctx.GetInt(ginTokens)
	thinkReason := env.Env.GetBool("server.think_reason")
	reasoningContent := ""

	onceExec := sync.OnceFunc(func() {
		if !sse {
			ctx.Writer.WriteHeader(http.StatusOK)
		}
	})

	var (
		matchers = common.GetGinMatchers(ctx)
	)

	defer r.Body.Close()
	reader := bufio.NewReader(r.Body)
	think := 0
	deepseekEvent := ""
	deepseekContentPatch := false
	deepseekFragmentType := ""
	for {
		dataBytes, err := readDeepSeekEventLine(reader)
		if err == io.EOF {
			break
		}

		if asError(ctx, err) {
			return
		}

		if bytes.HasPrefix(dataBytes, []byte("event: ")) {
			deepseekEvent = string(bytes.TrimSpace(dataBytes[7:]))
			if deepseekEvent == "finish" {
				break
			}
			continue
		}

		if bytes.HasPrefix(dataBytes, []byte("data: ")) {
			dataBytes = dataBytes[6:]
		}
		if len(dataBytes) == 0 {
			continue
		}

		var patch map[string]interface{}
		if err = json.Unmarshal(dataBytes, &patch); err == nil {
			path, hasPath := patch["p"].(string)
			if hasPath {
				deepseekContentPatch = isDeepSeekContentPatchPath(path)
				if path == "response/status" && patch["v"] == "FINISHED" {
					break
				}
			}

			var fragmentDeltas []deepSeekFragmentDelta
			if !hasPath {
				fragmentDeltas = deepSeekSnapshotDeltas(patch["v"])
				if len(fragmentDeltas) == 0 && deepseekContentPatch {
					if value, ok := patch["v"].(string); ok {
						fragmentDeltas = []deepSeekFragmentDelta{{Content: value, Type: deepseekFragmentType}}
					}
				}
			} else if deepseekContentPatch {
				if delta, ok := deepSeekContentPatchDelta(path, patch["v"], deepseekFragmentType); ok {
					fragmentDeltas = []deepSeekFragmentDelta{delta}
				}
			} else if path == "response/fragments" && patch["o"] == "APPEND" {
				fragmentDeltas = deepSeekFragmentDeltas(patch["v"])
			} else if path == "response" && patch["o"] == "BATCH" {
				fragmentDeltas = deepSeekBatchDeltas(patch["v"])
			}

			stopPatchStream := false
			for _, delta := range fragmentDeltas {
				if delta.Type != "" {
					deepseekFragmentType = delta.Type
				}
				if delta.Content == "" {
					continue
				}
				onceExec()
				if strings.EqualFold(deepseekFragmentType, "THINK") {
					if sse {
						response.ReasonSSEResponse(ctx, Model, "", delta.Content, created)
					}
					reasoningContent += delta.Content
					continue
				}

				raw := delta.Content
				raw = response.ExecMatchers(matchers, raw, false)
				if len(raw) == 0 {
					continue
				}
				if raw == response.EOF {
					stopPatchStream = true
					break
				}
				if sse {
					response.ReasonSSEResponse(ctx, Model, raw, "", created)
				}
				content += raw
			}
			if stopPatchStream {
				break
			}
			if len(fragmentDeltas) > 0 {
				continue
			}
		}

		var res model.Response

		err = json.Unmarshal(dataBytes, &res)
		if err != nil {
			logger.Warn(err)
			continue
		}

		if len(res.Choices) == 0 {
			continue
		}

		if res.Choices[0].FinishReason != nil && *res.Choices[0].FinishReason == "stop" {
			break
		}

		delta := res.Choices[0].Delta
		if delta.Type == "thinking" {
			if thinkReason {
				delta.ReasoningContent = delta.Content
				reasoningContent += delta.Content
				delta.Content = ""
				think = 1
			} else if think == 0 {
				think = 1
				delta.Content = "<think>\n" + delta.Content
			}
		} else {
			if thinkReason {
				think = 2
			} else if think == 1 {
				think = 2
				delta.Content = "\n</think>\n" + delta.Content
			}
		}

		raw := delta.Content
		if thinkReason && think == 1 {
			logger.Debug("----- think raw -----")
			logger.Debug(delta.ReasoningContent)
			goto label
		}

		logger.Debug("----- raw -----")
		logger.Debug(raw)
		onceExec()

		raw = response.ExecMatchers(matchers, raw, false)
		if len(raw) == 0 {
			continue
		}

	label:
		if raw == response.EOF {
			break
		}

		if sse {
			response.ReasonSSEResponse(ctx, Model, raw, delta.ReasoningContent, created)
		}
		content += raw
	}

	raw := response.ExecMatchers(matchers, "", true)
	if raw != "" && sse {
		response.SSEResponse(ctx, Model, raw, created)
	}
	content += raw

	if content == "" && response.NotSSEHeader(ctx) {
		return
	}
	ctx.Set(vars.GinCompletionUsage, response.CalcUsageTokens(reasoningContent+content, tokens))
	if !sse {
		response.ReasonResponse(ctx, Model, content, reasoningContent)
	} else {
		response.SSEResponse(ctx, Model, "[DONE]", created)
	}
	return
}

func readDeepSeekEventLine(reader *bufio.Reader) ([]byte, error) {
	fragment, isPrefix, err := reader.ReadLine()
	if len(fragment) > maxDeepSeekEventLineBytes {
		return nil, fmt.Errorf("DeepSeek event line exceeds %d MiB", maxDeepSeekEventLineBytes>>20)
	}
	if err != nil {
		if err == io.EOF && len(fragment) > 0 {
			return fragment, nil
		}
		return nil, err
	}
	if !isPrefix {
		return fragment, nil
	}
	line := append([]byte(nil), fragment...)
	for {
		fragment, isPrefix, err = reader.ReadLine()
		if len(fragment) > maxDeepSeekEventLineBytes-len(line) {
			return nil, fmt.Errorf("DeepSeek event line exceeds %d MiB", maxDeepSeekEventLineBytes>>20)
		}
		line = append(line, fragment...)
		if err != nil {
			if err == io.EOF && len(line) > 0 {
				return line, nil
			}
			return nil, err
		}
		if !isPrefix {
			return line, nil
		}
	}
}

func isDeepSeekContentPatchPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "response/content" {
		return true
	}
	if !strings.HasPrefix(path, "response/fragments/") {
		return false
	}
	remainder := strings.TrimPrefix(path, "response/fragments/")
	return !strings.Contains(remainder, "/") || strings.HasSuffix(remainder, "/content")
}

type deepSeekFragmentDelta struct {
	Content string
	Type    string
}

// DeepSeek starts a fragment by appending the whole object, then streams later
// text as direct /content patches. THINK and RESPONSE fragments share the same
// paths, so the parser must retain the current fragment type across pathless
// inherited patches.
func deepSeekContentPatchDelta(pathValue, value interface{}, activeType string) (deepSeekFragmentDelta, bool) {
	path, _ := pathValue.(string)
	path = strings.TrimSpace(path)
	if !isDeepSeekContentPatchPath(path) {
		return deepSeekFragmentDelta{}, false
	}

	if text, ok := value.(string); ok {
		if path == "response/content" {
			activeType = "RESPONSE"
		}
		return deepSeekFragmentDelta{Content: text, Type: activeType}, path == "response/content" || strings.HasSuffix(path, "/content")
	}

	return deepSeekFragmentDeltaFromValue(value)
}

func deepSeekSnapshotDeltas(value interface{}) []deepSeekFragmentDelta {
	root, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	responseValue, ok := root["response"].(map[string]interface{})
	if !ok {
		return nil
	}
	if content, ok := responseValue["content"].(string); ok && content != "" {
		return []deepSeekFragmentDelta{{Content: content, Type: "RESPONSE"}}
	}
	return deepSeekFragmentDeltas(responseValue["fragments"])
}

func deepSeekBatchDeltas(value interface{}) []deepSeekFragmentDelta {
	operations, ok := value.([]interface{})
	if !ok {
		return nil
	}
	var deltas []deepSeekFragmentDelta
	for _, rawOperation := range operations {
		operation, ok := rawOperation.(map[string]interface{})
		if !ok || operation["p"] != "fragments" || operation["o"] != "APPEND" {
			continue
		}
		deltas = append(deltas, deepSeekFragmentDeltas(operation["v"])...)
	}
	return deltas
}

func deepSeekFragmentDeltas(value interface{}) []deepSeekFragmentDelta {
	fragments, ok := value.([]interface{})
	if !ok {
		if delta, ok := deepSeekFragmentDeltaFromValue(value); ok {
			return []deepSeekFragmentDelta{delta}
		}
		return nil
	}

	deltas := make([]deepSeekFragmentDelta, 0, len(fragments))
	for _, rawFragment := range fragments {
		if delta, ok := deepSeekFragmentDeltaFromValue(rawFragment); ok {
			deltas = append(deltas, delta)
		}
	}
	return deltas
}

func deepSeekFragmentDeltaFromValue(value interface{}) (deepSeekFragmentDelta, bool) {
	fragment, ok := value.(map[string]interface{})
	if !ok {
		return deepSeekFragmentDelta{}, false
	}
	content, _ := fragment["content"].(string)
	fragmentType, _ := fragment["type"].(string)
	fragmentType = strings.ToUpper(strings.TrimSpace(fragmentType))
	if content == "" && fragmentType == "" {
		return deepSeekFragmentDelta{}, false
	}
	return deepSeekFragmentDelta{Content: content, Type: fragmentType}, true
}

func asError(ctx *gin.Context, err error) (ok bool) {
	if err == nil {
		return
	}

	logger.Error(err)
	if response.NotSSEHeader(ctx) {
		response.Error(ctx, -1, err)
	}
	ok = true
	return
}
