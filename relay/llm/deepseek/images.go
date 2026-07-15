package deepseek

import (
	"bytes"
	"chatgpt-adapter/core/common"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/bincooo/emit.io"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxDeepSeekImageBytes = 20 << 20

func uploadDeepSeekImages(ctx context.Context, proxied, cookie string, mode deepSeekMode, imageURLs []string) ([]string, error) {
	fileIDs := make([]string, 0, len(imageURLs))
	for _, imageURL := range imageURLs {
		fileID, err := uploadDeepSeekImage(ctx, proxied, cookie, mode, imageURL)
		if err != nil {
			return nil, err
		}
		fileIDs = append(fileIDs, fileID)
	}
	return fileIDs, nil
}

func uploadDeepSeekImage(ctx context.Context, proxied, cookie string, mode deepSeekMode, imageURL string) (string, error) {
	data, contentType, filename, err := loadDeepSeekImage(proxied, imageURL)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("DeepSeek image input is empty")
	}
	if len(data) > maxDeepSeekImageBytes {
		return "", fmt.Errorf("DeepSeek image input exceeds %d MiB", maxDeepSeekImageBytes>>20)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename=%q`, filename)}
	header["Content-Type"] = []string{contentType}
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err = io.Copy(part, bytes.NewReader(data)); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	thinkingHeader := "0"
	if mode.ThinkingEnabled {
		thinkingHeader = "1"
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		powHeader, powErr := createPoWResponse(ctx, proxied, cookie, "/api/v0/file/upload_file", true)
		if powErr != nil {
			lastErr = fmt.Errorf("DeepSeek image upload PoW challenge: %w", powErr)
			continue
		}
		response, requestErr := emit.ClientBuilder(common.HTTPClient).
			Context(ctx).
			Proxies(proxied).
			POST("https://chat.deepseek.com/api/v0/file/upload_file").
			Ja3().
			Header("authorization", "Bearer "+cookie).
			Header("content-type", writer.FormDataContentType()).
			Header("accept", "application/json").
			Header("origin", "https://chat.deepseek.com").
			Header("referer", "https://chat.deepseek.com/").
			Header("user-agent", webUserAgent).
			Header("x-client-bundle-id", "com.deepseek.chat").
			Header("x-client-locale", "zh_CN").
			Header("x-client-platform", "web").
			Header("x-client-version", webClientVersion).
			Header("x-client-timezone-offset", "28800").
			Header("x-thinking-enabled", thinkingHeader).
			Header("x-model-type", mode.ModelType).
			Header("x-file-size", strconv.Itoa(len(data))).
			Header("x-ds-pow-response", powHeader).
			Header(elseOf(clearance != "", "cookie"), clearance).
			Header(elseOf(lang != "", "accept-language"), lang).
			Bytes(body.Bytes()).
			Do()
		if requestErr != nil {
			lastErr = fmt.Errorf("DeepSeek image upload request: %w", requestErr)
			continue
		}
		if response.StatusCode != http.StatusOK {
			responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			_ = response.Body.Close()
			if readErr != nil {
				lastErr = fmt.Errorf("DeepSeek image upload request: HTTP %d", response.StatusCode)
			} else {
				message := strings.TrimSpace(string(responseBody))
				if len(message) > 500 {
					message = message[:500]
				}
				lastErr = fmt.Errorf("DeepSeek image upload request: HTTP %d: %s", response.StatusCode, message)
			}
			continue
		}

		obj, parseErr := emit.ToMap(response)
		_ = response.Body.Close()
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		if code, ok := obj["code"].(float64); ok && code != 0 {
			lastErr = deepSeekResponseError(obj, "DeepSeek image upload failed")
			continue
		}
		fileID, status := extractDeepSeekFile(deepSeekBizData(obj), "")
		if fileID == "" {
			lastErr = errors.New("DeepSeek image upload succeeded without file id")
			continue
		}
		if isDeepSeekFileReady(status) {
			return fileID, nil
		}
		if err = waitForDeepSeekFile(ctx, proxied, cookie, fileID); err != nil {
			return "", err
		}
		return fileID, nil
	}

	if lastErr == nil {
		lastErr = errors.New("DeepSeek image upload failed")
	}
	return "", lastErr
}

func loadDeepSeekImage(proxied, imageURL string) ([]byte, string, string, error) {
	imageURL = strings.TrimSpace(imageURL)
	if strings.HasPrefix(imageURL, "data:") {
		comma := strings.IndexByte(imageURL, ',')
		if comma <= 5 {
			return nil, "", "", errors.New("invalid DeepSeek image data URI")
		}
		meta, payload := imageURL[5:comma], imageURL[comma+1:]
		if !strings.Contains(meta, ";base64") {
			return nil, "", "", errors.New("DeepSeek image data URI must be base64 encoded")
		}
		contentType := strings.TrimSpace(strings.Split(meta, ";")[0])
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", "", fmt.Errorf("decode DeepSeek image: %w", err)
		}
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		return data, contentType, deepSeekImageFilename("image", contentType), nil
	}

	parsed, err := url.Parse(imageURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", "", errors.New("DeepSeek image URL must use data, http, or https")
	}
	data, err := common.DownloadBuffer(common.HTTPClient, proxied, imageURL, map[string]string{"User-Agent": userAgent})
	if err != nil {
		return nil, "", "", fmt.Errorf("download DeepSeek image: %w", err)
	}
	contentType := http.DetectContentType(data)
	filename := filepath.Base(parsed.Path)
	if filename == "." || filename == "/" || filename == "" {
		filename = "image"
	}
	return data, contentType, deepSeekImageFilename(filename, contentType), nil
}

func deepSeekImageFilename(filename, contentType string) string {
	filename = filepath.Base(strings.TrimSpace(filename))
	filename = strings.ReplaceAll(filename, `\`, "_")
	filename = strings.ReplaceAll(filename, "\"", "_")
	if filepath.Ext(filename) == "" {
		if extensions, err := mime.ExtensionsByType(contentType); err == nil && len(extensions) > 0 {
			filename += extensions[0]
		} else {
			filename += ".bin"
		}
	}
	return filename
}

func waitForDeepSeekFile(ctx context.Context, proxied, cookie, fileID string) error {
	for attempt := 0; attempt < 30; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		response, err := emit.ClientBuilder(common.HTTPClient).
			Context(ctx).
			Proxies(proxied).
			GET("https://chat.deepseek.com/api/v0/file/fetch_files").
			Query("file_ids", fileID).
			Ja3().
			Header("authorization", "Bearer "+cookie).
			Header("accept", "application/json").
			Header("origin", "https://chat.deepseek.com").
			Header("referer", "https://chat.deepseek.com/").
			Header("user-agent", webUserAgent).
			Header("accept-charset", "UTF-8").
			Header("x-client-bundle-id", "com.deepseek.chat").
			Header("x-client-locale", "zh_CN").
			Header("x-client-platform", "web").
			Header("x-client-version", webClientVersion).
			Header("x-client-timezone-offset", "28800").
			Header(elseOf(clearance != "", "cookie"), clearance).
			Header(elseOf(lang != "", "accept-language"), lang).
			DoC(emit.Status(http.StatusOK), emit.IsJSON)
		if err == nil {
			obj, parseErr := emit.ToMap(response)
			_ = response.Body.Close()
			if parseErr == nil {
				_, status := extractDeepSeekFile(deepSeekBizData(obj), fileID)
				if isDeepSeekFileReady(status) {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("DeepSeek image file %s did not become ready", fileID)
}

func deepSeekBizData(obj map[string]interface{}) interface{} {
	data, ok := obj["data"].(map[string]interface{})
	if !ok {
		return obj
	}
	if bizData, ok := data["biz_data"].(map[string]interface{}); ok {
		return bizData
	}
	return data
}

func deepSeekResponseError(obj map[string]interface{}, fallback string) error {
	for _, key := range []string{"biz_msg", "msg", "message"} {
		if message, ok := obj[key].(string); ok && strings.TrimSpace(message) != "" {
			return errors.New(strings.TrimSpace(message))
		}
	}
	return errors.New(fallback)
}

func extractDeepSeekFile(value interface{}, targetID string) (string, string) {
	switch typed := value.(type) {
	case map[string]interface{}:
		fileID := deepSeekString(typed["file_id"])
		if fileID == "" {
			fileID = deepSeekString(typed["id"])
		}
		if fileID != "" && (targetID == "" || strings.EqualFold(fileID, targetID)) {
			status := deepSeekString(typed["status"])
			if status == "" {
				status = deepSeekString(typed["file_status"])
			}
			return fileID, status
		}
		for _, nested := range typed {
			if nestedID, status := extractDeepSeekFile(nested, targetID); nestedID != "" {
				return nestedID, status
			}
		}
	case []interface{}:
		for _, nested := range typed {
			if nestedID, status := extractDeepSeekFile(nested, targetID); nestedID != "" {
				return nestedID, status
			}
		}
	}
	return "", ""
}

func deepSeekString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func isDeepSeekFileReady(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "processed", "ready", "done", "available", "success", "completed", "finished":
		return true
	default:
		return false
	}
}
