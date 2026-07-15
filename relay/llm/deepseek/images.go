package deepseek

import (
	"bytes"
	"chatgpt-adapter/core/common"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/bincooo/emit.io"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxDeepSeekImageBytes = 20 << 20

var blockedDeepSeekImagePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2002::/16"),
}

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
	data, contentType, filename, err := loadDeepSeekImage(ctx, proxied, imageURL)
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

func loadDeepSeekImage(ctx context.Context, proxied, imageURL string) ([]byte, string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	imageURL = strings.TrimSpace(imageURL)
	if strings.HasPrefix(imageURL, "data:") {
		comma := strings.IndexByte(imageURL, ',')
		if comma <= 5 {
			return nil, "", "", errors.New("invalid DeepSeek image data URI")
		}
		meta, payload := imageURL[5:comma], imageURL[comma+1:]
		metaParts := strings.Split(meta, ";")
		contentType := strings.TrimSpace(metaParts[0])
		base64Encoded := false
		for _, option := range metaParts[1:] {
			if strings.EqualFold(strings.TrimSpace(option), "base64") {
				base64Encoded = true
				break
			}
		}
		if !base64Encoded {
			return nil, "", "", errors.New("DeepSeek image data URI must be base64 encoded")
		}
		if !isDeepSeekImageContentType(contentType) {
			return nil, "", "", errors.New("DeepSeek image data URI must use an image media type")
		}
		data, err := readDeepSeekImage(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)), -1)
		if err != nil {
			return nil, "", "", fmt.Errorf("decode DeepSeek image: %w", err)
		}
		return data, contentType, deepSeekImageFilename("image", contentType), nil
	}

	parsed, err := url.Parse(imageURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", "", errors.New("DeepSeek image URL must use data, http, or https")
	}
	if err = validateDeepSeekImageURL(ctx, parsed); err != nil {
		return nil, "", "", err
	}
	response, err := downloadDeepSeekImage(ctx, proxied, parsed)
	if err != nil {
		return nil, "", "", fmt.Errorf("download DeepSeek image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("download DeepSeek image: HTTP %d", response.StatusCode)
	}
	data, err := readDeepSeekImage(response.Body, response.ContentLength)
	if err != nil {
		return nil, "", "", fmt.Errorf("download DeepSeek image: %w", err)
	}
	contentType := response.Header.Get("Content-Type")
	if !isDeepSeekImageContentType(contentType) {
		contentType = http.DetectContentType(data)
	}
	if !isDeepSeekImageContentType(contentType) {
		return nil, "", "", errors.New("download DeepSeek image: response is not an image")
	}
	filename := filepath.Base(parsed.Path)
	if filename == "." || filename == "/" || filename == "" {
		filename = "image"
	}
	return data, contentType, deepSeekImageFilename(filename, contentType), nil
}

func validateDeepSeekImageURL(ctx context.Context, parsed *url.URL) error {
	_, err := resolveDeepSeekImageAddress(ctx, parsed)
	return err
}

func resolveDeepSeekImageAddress(ctx context.Context, parsed *url.URL) (netip.Addr, error) {
	if parsed.User != nil {
		return netip.Addr{}, errors.New("DeepSeek image URL must not contain credentials")
	}
	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" {
		return netip.Addr{}, errors.New("DeepSeek image URL is missing a hostname")
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if !isPublicDeepSeekImageIP(ip) {
			return netip.Addr{}, errors.New("DeepSeek image URL resolves to a non-public address")
		}
		address, _ := netip.AddrFromSlice(ip)
		return address.Unmap(), nil
	}

	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve DeepSeek image URL: %w", err)
	}
	if len(addresses) == 0 {
		return netip.Addr{}, errors.New("resolve DeepSeek image URL: no addresses returned")
	}
	var selected netip.Addr
	for _, address := range addresses {
		if !isPublicDeepSeekImageIP(address.IP) {
			return netip.Addr{}, errors.New("DeepSeek image URL resolves to a non-public address")
		}
		if !selected.IsValid() {
			selected, _ = netip.AddrFromSlice(address.IP)
			selected = selected.Unmap()
		}
	}
	return selected, nil
}

func downloadDeepSeekImage(ctx context.Context, proxied string, parsed *url.URL) (*http.Response, error) {
	address, err := resolveDeepSeekImageAddress(ctx, parsed)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: parsed.Hostname(),
	}
	if strings.TrimSpace(proxied) != "" {
		proxyURL, parseErr := url.Parse(proxied)
		if parseErr != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
			return nil, errors.New("download DeepSeek image: invalid proxy URL")
		}
		switch strings.ToLower(proxyURL.Scheme) {
		case "http", "socks5", "socks5h":
		default:
			return nil, errors.New("download DeepSeek image: unsupported proxy scheme")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	pinnedURL := *parsed
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	pinnedURL.Host = net.JoinHostPort(address.String(), port)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pinnedURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Host = parsed.Host
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client.Do(request)
}

func isPublicDeepSeekImageIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedDeepSeekImagePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func readDeepSeekImage(reader io.Reader, contentLength int64) ([]byte, error) {
	if contentLength > maxDeepSeekImageBytes {
		return nil, fmt.Errorf("DeepSeek image input exceeds %d MiB", maxDeepSeekImageBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxDeepSeekImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDeepSeekImageBytes {
		return nil, fmt.Errorf("DeepSeek image input exceeds %d MiB", maxDeepSeekImageBytes>>20)
	}
	return data, nil
}

func isDeepSeekImageContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.HasPrefix(strings.ToLower(mediaType), "image/")
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
