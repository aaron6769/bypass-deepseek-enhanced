package deepseek

import (
	"bytes"
	"chatgpt-adapter/core/common"
	"chatgpt-adapter/core/gin/model"
	"chatgpt-adapter/core/gin/response"
	"chatgpt-adapter/core/logger"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bincooo/emit.io"
	"github.com/gin-gonic/gin"
	"github.com/iocgo/sdk/env"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	userAgent    = "DeepSeek/2.0.4 Android/35"
	webUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	lang         string
	clearance    string

	mu      sync.Mutex
	powSlot = make(chan struct{}, 1)
)

const webClientVersion = "2.2.0"

type deepseekRequest struct {
	ChatSessionId   string   `json:"chat_session_id"`
	ModelType       string   `json:"model_type"`
	ParentMessageId *int     `json:"parent_message_id"`
	Message         string   `json:"prompt"`
	RefFileIds      []string `json:"ref_file_ids"`
	ThinkingEnabled bool     `json:"thinking_enabled"`
	SearchEnabled   bool     `json:"search_enabled"`
}

func fetch(ctx context.Context, proxied, cookie string, request deepseekRequest) (response *http.Response, err error) {
	retry := 3
label:
	retry--
	powHeader, err := createPoWResponse(ctx, proxied, cookie, "/api/v0/chat/completion")
	if err != nil {
		if shouldRetryDeepSeek(err, retry) {
			goto label
		}
		return
	}

	response, err = emit.ClientBuilder(common.HTTPClient).
		Context(ctx).
		Proxies(proxied).
		POST("https://chat.deepseek.com/api/v0/chat/completion").
		JSONHeader().
		Ja3().
		Header("authorization", "Bearer "+cookie).
		Header("origin", "https://chat.deepseek.com").
		Header("referer", "https://chat.deepseek.com/").
		Header("user-agent", userAgent).
		Header("accept-charset", "UTF-8").
		Header("x-client-locale", "zh_CN").
		Header("x-client-platform", "android").
		Header("x-client-version", "2.0.4").
		Header("x-ds-pow-response", powHeader).
		Header(elseOf(clearance != "", "cookie"), clearance).
		Header(elseOf(lang != "", "accept-language"), lang).
		Body(request).
		DoC(emit.Status(http.StatusOK), emit.IsSTREAM)
	if err != nil {
		if shouldRetryDeepSeek(err, retry) {
			goto label
		}
	}
	return
}

func createPoWResponse(ctx context.Context, proxied, cookie, targetPath string, webProfile ...bool) (string, error) {
	powUserAgent := userAgent
	if len(webProfile) > 0 && webProfile[0] {
		powUserAgent = webUserAgent
	}
	builder := emit.ClientBuilder(common.HTTPClient).
		Context(ctx).
		Proxies(proxied).
		POST("https://chat.deepseek.com/api/v0/chat/create_pow_challenge").
		JSONHeader().
		Ja3().
		Header("authorization", "Bearer "+cookie).
		Header("origin", "https://chat.deepseek.com").
		Header("referer", "https://chat.deepseek.com/").
		Header("user-agent", powUserAgent).
		Header("accept-charset", "UTF-8")
	if len(webProfile) > 0 && webProfile[0] {
		builder.
			Header("x-client-bundle-id", "com.deepseek.chat").
			Header("x-client-locale", "zh_CN").
			Header("x-client-platform", "web").
			Header("x-client-version", webClientVersion).
			Header("x-client-timezone-offset", "28800")
	} else {
		builder.
			Header("x-client-locale", "zh_CN").
			Header("x-client-platform", "android").
			Header("x-client-version", "2.0.4")
	}
	response, err := builder.
		Header(elseOf(clearance != "", "cookie"), clearance).
		Header(elseOf(lang != "", "accept-language"), lang).
		Body(map[string]interface{}{"target_path": targetPath}).
		DoC(emit.Status(http.StatusOK), emit.IsJSON)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	obj, err := emit.ToMap(response)
	if err != nil {
		return "", err
	}
	if code, ok := obj["code"].(float64); !ok || code != 0 {
		if msg, ok := obj["msg"].(string); ok && msg != "" {
			return "", errors.New(msg)
		}
		return "", errors.New("create challenge failed")
	}

	data, ok := obj["data"].(map[string]interface{})
	if !ok {
		return "", errors.New("create challenge failed: missing data")
	}
	bizData, ok := data["biz_data"].(map[string]interface{})
	if !ok {
		return "", errors.New("create challenge failed: missing biz_data")
	}
	challenge, ok := bizData["challenge"].(map[string]interface{})
	if !ok {
		return "", errors.New("create challenge failed: missing challenge")
	}

	answer, err := calcAnswer(ctx, challenge)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]interface{}{
		"algorithm":   "DeepSeekHashV1",
		"challenge":   challenge["challenge"],
		"salt":        challenge["salt"],
		"answer":      answer,
		"signature":   challenge["signature"],
		"target_path": targetPath,
	})
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

func shouldRetryDeepSeek(err error, retry int) bool {
	if err == nil || retry <= 0 {
		return false
	}

	var busErr emit.Error
	if errors.As(err, &busErr) {
		if strings.Contains(busErr.Msg, "code\":40300,\"msg\":\"Missing Header") || strings.Contains(busErr.Msg, "code\":40300,\"msg\":\"MISSING_HEADER") || strings.Contains(strings.ToLower(busErr.Msg), "eof") {
			logger.Error(err)
			time.Sleep(500 * time.Millisecond)
			return true
		}
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "eof") || strings.Contains(msg, "connection reset") || strings.Contains(msg, "timeout") {
		logger.Error(err)
		time.Sleep(500 * time.Millisecond)
		return true
	}

	return false
}

func deleteSession(proxied, cookie, sessionId string) {
	if strings.TrimSpace(sessionId) == "" {
		return
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r, err := emit.ClientBuilder(common.HTTPClient).
		Context(cleanupCtx).
		Proxies(proxied).
		POST("https://chat.deepseek.com/api/v0/chat_session/delete").
		JSONHeader().
		Ja3().
		Header("authorization", "Bearer "+cookie).
		Header("referer", "https://chat.deepseek.com/").
		Header("user-agent", userAgent).
		Header("accept-charset", "UTF-8").
		Header("x-client-locale", "zh_CN").
		Header("x-client-platform", "android").
		Header("x-client-version", "2.0.4").
		Header(elseOf(clearance != "", "cookie"), clearance).
		Header(elseOf(lang != "", "accept-language"), lang).
		Body(map[string]interface{}{
			"chat_session_id": sessionId,
		}).DoC(emit.Status(http.StatusOK), emit.IsJSON)
	if r != nil && r.Body != nil {
		defer r.Body.Close()
	}
	if err != nil {
		logger.Error(err)
	}
}

func calcAnswer(ctx context.Context, data map[string]interface{}) (num int, err error) {
	challenge, ok := data["challenge"].(string)
	if !ok || challenge == "" {
		return 0, errors.New("invalid DeepSeek PoW challenge")
	}
	salt, ok := data["salt"].(string)
	if !ok || salt == "" {
		return 0, errors.New("invalid DeepSeek PoW salt")
	}
	difficultyValue, ok := data["difficulty"].(float64)
	if !ok || difficultyValue <= 0 {
		return 0, errors.New("invalid DeepSeek PoW difficulty")
	}
	expireAtValue, ok := data["expire_at"].(float64)
	if !ok || expireAtValue <= 0 {
		return 0, errors.New("invalid DeepSeek PoW expiration")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case powSlot <- struct{}{}:
		defer func() { <-powSlot }()
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	started := time.Now()
	answer, err := solveDeepSeekPOW(ctx, challenge, salt, int64(expireAtValue), int64(difficultyValue))
	if err != nil {
		return 0, err
	}
	logger.Infof("DeepSeek local PoW solved in %s", time.Since(started))
	return int(answer), nil
}

func convertRequest(ctx *gin.Context, env *env.Environment, completion model.Completion) (request deepseekRequest, err error) {
	mode, ok := resolveDeepSeekMode(completion.Model)
	if !ok {
		return request, fmt.Errorf("unsupported DeepSeek model: %s", completion.Model)
	}
	proxied := env.GetString("server.proxied")
	cookie := ctx.GetString("token")

	prompt, imageURLs := buildDeepSeekPrompt(ctx, completion.Messages)
	if len(imageURLs) > 0 && !mode.VisionEnabled {
		return request, errors.New("DeepSeek image input requires deepseek-v4-vision or deepseek-v4-vision-nothinking")
	}

	retry := 3
retryCreateSession:
	retry--
	r, err := emit.ClientBuilder(common.HTTPClient).
		Context(ctx.Request.Context()).
		Proxies(proxied).
		POST("https://chat.deepseek.com/api/v0/chat_session/create").
		JSONHeader().
		Ja3().
		Header("authorization", "Bearer "+cookie).
		Header("referer", "https://chat.deepseek.com/").
		Header("user-agent", userAgent).
		Header("accept-charset", "UTF-8").
		Header("x-client-locale", "zh_CN").
		Header("x-client-platform", "android").
		Header("x-client-version", "2.0.4").
		Header(elseOf(clearance != "", "cookie"), clearance).
		Header(elseOf(lang != "", "accept-language"), lang).
		Body(map[string]interface{}{
			"character_id": nil,
		}).DoC(emit.Status(http.StatusOK), emit.IsJSON)
	if err != nil {
		var busErr emit.Error
		if errors.As(err, &busErr) && busErr.Code == 403 {
			_ = hookCloudflare(env)
		}
		if shouldRetryDeepSeek(err, retry) {
			goto retryCreateSession
		}
		return
	}

	defer r.Body.Close()
	obj, err := emit.ToMap(r)
	if err != nil {
		return
	}

	if code, ok := obj["code"].(float64); !ok || code != 0 {
		err = errors.New("create chat session failed")
		if msg, ok := obj["msg"].(string); ok && msg != "" {
			err = errors.New(msg)
		}
		return
	}

	responseData, ok := obj["data"].(map[string]interface{})
	if !ok {
		err = errors.New("create chat session failed: missing data")
		return
	}
	data, ok := responseData["biz_data"].(map[string]interface{})
	if !ok {
		err = errors.New("create chat session failed: missing biz_data")
		return
	}
	sessionID := extractDeepSeekSessionID(data)
	if sessionID == "" {
		err = errors.New("create chat session failed: missing session id")
		return
	}
	cleanupSession := true
	defer func() {
		if cleanupSession {
			deleteSession(proxied, cookie, sessionID)
		}
	}()

	refFileIDs := make([]string, 0, len(imageURLs))
	if len(imageURLs) > 0 {
		refFileIDs, err = uploadDeepSeekImages(ctx.Request.Context(), proxied, cookie, mode, imageURLs)
		if err != nil {
			return request, err
		}
	}

	request = deepseekRequest{
		ChatSessionId:   sessionID,
		ModelType:       mode.ModelType,
		RefFileIds:      refFileIDs,
		ThinkingEnabled: mode.ThinkingEnabled,
		SearchEnabled:   mode.SearchEnabled,
		Message:         prompt,
	}
	cleanupSession = false
	return
}

func extractDeepSeekSessionID(bizData map[string]interface{}) string {
	if sessionID, _ := bizData["id"].(string); strings.TrimSpace(sessionID) != "" {
		return strings.TrimSpace(sessionID)
	}
	if chatSession, ok := bizData["chat_session"].(map[string]interface{}); ok {
		if sessionID, _ := chatSession["id"].(string); strings.TrimSpace(sessionID) != "" {
			return strings.TrimSpace(sessionID)
		}
	}
	return ""
}

func buildDeepSeekPrompt(ctx *gin.Context, messages []model.Keyv[interface{}]) (string, []string) {
	contentBuffer := new(bytes.Buffer)
	imageURLs := make([]string, 0)

	for index, message := range messages {
		text, urls := deepSeekMessageContent(message)
		imageURLs = append(imageURLs, urls...)
		if len(messages) == 1 && index == 0 {
			contentBuffer.WriteString(text)
			continue
		}
		role, end := response.ConvertRole(ctx, message.GetString("role"))
		contentBuffer.WriteString(role)
		contentBuffer.WriteString(text)
		contentBuffer.WriteString(end)
	}

	return contentBuffer.String(), imageURLs
}

func deepSeekMessageContent(message model.Keyv[interface{}]) (string, []string) {
	if message.IsString("content") {
		return message.GetString("content"), nil
	}

	contentBuffer := new(bytes.Buffer)
	imageURLs := make([]string, 0)
	for _, rawPart := range message.GetSlice("content") {
		part, ok := rawPart.(map[string]interface{})
		if !ok {
			continue
		}
		switch part["type"] {
		case "text", "input_text":
			if text, ok := part["text"].(string); ok {
				contentBuffer.WriteString(text)
			}
		case "image_url", "input_image":
			if imageURL, ok := part["image_url"].(string); ok && imageURL != "" {
				imageURLs = append(imageURLs, imageURL)
				continue
			}
			if image, ok := part["image_url"].(map[string]interface{}); ok {
				if imageURL, ok := image["url"].(string); ok && imageURL != "" {
					imageURLs = append(imageURLs, imageURL)
				}
			}
		}
	}
	return contentBuffer.String(), imageURLs
}

func hookCloudflare(env *env.Environment) error {
	if clearance != "" {
		return nil
	}

	baseUrl := env.GetString("browser-less.reversal")
	if !env.GetBool("browser-less.enabled") && baseUrl == "" {
		return errors.New("trying cloudflare failed, please setting `browser-less.enabled` or `browser-less.reversal`")
	}

	logger.Info("trying cloudflare ...")

	mu.Lock()
	defer mu.Unlock()
	if clearance != "" {
		return nil
	}

	if baseUrl == "" {
		baseUrl = "http://127.0.0.1:" + env.GetString("browser-less.port")
	}

	r, err := emit.ClientBuilder(common.HTTPClient).
		GET(baseUrl+"/v0/clearance").
		Header("x-website", "https://chat.deepseek.com").
		DoC(emit.Status(http.StatusOK), emit.IsJSON)
	if err != nil {
		logger.Error(err)
		if emit.IsJSON(r) == nil {
			logger.Error(emit.TextResponse(r))
		}
		return err
	}

	defer r.Body.Close()
	obj, err := emit.ToMap(r)
	if err != nil {
		logger.Error(err)
		return err
	}

	data := obj["data"].(map[string]interface{})
	clearance = data["cookie"].(string)
	userAgent = data["userAgent"].(string)
	lang = data["lang"].(string)
	return nil
}

func elseOf[T any](condition bool, t T) (zero T) {
	if condition {
		return t
	}
	return zero
}
