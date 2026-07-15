package deepseek

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

const (
	deepSeekImageDownloadTimeout = 30 * time.Second
	deepSeekImageHeaderTimeout   = 10 * time.Second
	deepSeekImageMaxRedirects    = 3
)

var deepSeekBlockedPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func loadDeepSeekImage(ctx context.Context, proxied, imageURL string) ([]byte, string, string, error) {
	imageURL = strings.TrimSpace(imageURL)
	if strings.HasPrefix(strings.ToLower(imageURL), "data:") {
		return decodeDeepSeekDataImage(imageURL, maxDeepSeekImageBytes)
	}

	downloadCtx, cancel := context.WithTimeout(ctx, deepSeekImageDownloadTimeout)
	defer cancel()

	data, contentType, finalURL, err := downloadDeepSeekImage(downloadCtx, proxied, imageURL, maxDeepSeekImageBytes)
	if err != nil {
		return nil, "", "", err
	}
	filename := filepath.Base(finalURL.Path)
	if filename == "." || filename == "/" || filename == "" {
		filename = "image"
	}
	return data, contentType, deepSeekImageFilename(filename, contentType), nil
}

func decodeDeepSeekDataImage(imageURL string, maxBytes int64) ([]byte, string, string, error) {
	comma := strings.IndexByte(imageURL, ',')
	if comma <= 5 {
		return nil, "", "", errors.New("invalid DeepSeek image data URI")
	}
	meta, payload := imageURL[5:comma], imageURL[comma+1:]
	parts := strings.Split(meta, ";")
	if len(parts) < 2 || !strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "base64") {
		return nil, "", "", errors.New("DeepSeek image data URI must be base64 encoded")
	}
	contentType, _, err := mime.ParseMediaType(strings.Join(parts[:len(parts)-1], ";"))
	if err != nil || !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return nil, "", "", errors.New("DeepSeek data URI must contain an image media type")
	}
	if int64(len(payload)) > int64(base64.StdEncoding.EncodedLen(int(maxBytes+1))) {
		return nil, "", "", fmt.Errorf("DeepSeek image input exceeds %d MiB", maxBytes>>20)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("decode DeepSeek image: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", "", fmt.Errorf("DeepSeek image input exceeds %d MiB", maxBytes>>20)
	}
	return data, contentType, deepSeekImageFilename("image", contentType), nil
}

func downloadDeepSeekImage(ctx context.Context, proxied, rawURL string, maxBytes int64) ([]byte, string, *url.URL, error) {
	current, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", nil, errors.New("invalid DeepSeek image URL")
	}

	for redirect := 0; redirect <= deepSeekImageMaxRedirects; redirect++ {
		pinned, hostHeader, serverName, err := pinDeepSeekImageURL(ctx, current)
		if err != nil {
			return nil, "", nil, err
		}
		client, transport, err := newDeepSeekImageHTTPClient(proxied, serverName)
		if err != nil {
			return nil, "", nil, err
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, pinned.String(), nil)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, "", nil, fmt.Errorf("create DeepSeek image request: %w", err)
		}
		request.Host = hostHeader
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*")

		response, err := client.Do(request)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, "", nil, fmt.Errorf("download DeepSeek image: %w", err)
		}

		if isDeepSeekRedirect(response.StatusCode) {
			location := response.Header.Get("Location")
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
			_ = response.Body.Close()
			transport.CloseIdleConnections()
			if location == "" {
				return nil, "", nil, errors.New("DeepSeek image redirect is missing Location")
			}
			if redirect == deepSeekImageMaxRedirects {
				return nil, "", nil, errors.New("DeepSeek image exceeded redirect limit")
			}
			next, parseErr := current.Parse(location)
			if parseErr != nil {
				return nil, "", nil, errors.New("invalid DeepSeek image redirect")
			}
			current = next
			continue
		}

		if response.StatusCode != http.StatusOK {
			message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
			_ = response.Body.Close()
			transport.CloseIdleConnections()
			return nil, "", nil, fmt.Errorf("download DeepSeek image: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
		}
		if response.ContentLength > maxBytes {
			_ = response.Body.Close()
			transport.CloseIdleConnections()
			return nil, "", nil, fmt.Errorf("DeepSeek image input exceeds %d MiB", maxBytes>>20)
		}

		data, readErr := readDeepSeekImageBody(response.Body, maxBytes)
		_ = response.Body.Close()
		transport.CloseIdleConnections()
		if readErr != nil {
			return nil, "", nil, readErr
		}
		contentType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])
		if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
			contentType = http.DetectContentType(data)
		}
		if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
			return nil, "", nil, errors.New("DeepSeek image URL did not return an image")
		}
		return data, contentType, current, nil
	}

	return nil, "", nil, errors.New("DeepSeek image exceeded redirect limit")
}

func readDeepSeekImageBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read DeepSeek image: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("DeepSeek image input exceeds %d MiB", maxBytes>>20)
	}
	return data, nil
}

func pinDeepSeekImageURL(ctx context.Context, source *url.URL) (*url.URL, string, string, error) {
	if source == nil {
		return nil, "", "", errors.New("DeepSeek image URL must use http or https")
	}
	scheme := strings.ToLower(source.Scheme)
	if (scheme != "http" && scheme != "https") || source.Hostname() == "" {
		return nil, "", "", errors.New("DeepSeek image URL must use http or https")
	}
	if source.User != nil {
		return nil, "", "", errors.New("DeepSeek image URL must not contain credentials")
	}

	hostname := source.Hostname()
	port := source.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, "", "", errors.New("DeepSeek image URL contains an invalid port")
	}

	addresses, err := resolveDeepSeekImageHost(ctx, hostname)
	if err != nil {
		return nil, "", "", err
	}
	for _, address := range addresses {
		if !isPublicDeepSeekImageIP(address) {
			return nil, "", "", fmt.Errorf("DeepSeek image URL resolves to a non-public address: %s", address)
		}
	}

	pinned := *source
	pinned.Scheme = scheme
	pinned.Host = net.JoinHostPort(addresses[0].String(), port)
	return &pinned, source.Host, hostname, nil
}

func resolveDeepSeekImageHost(ctx context.Context, hostname string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(hostname); err == nil {
		return []netip.Addr{address.Unmap()}, nil
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve DeepSeek image host: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("resolve DeepSeek image host: no addresses returned")
	}
	for index := range addresses {
		addresses[index] = addresses[index].Unmap()
	}
	return addresses, nil
}

func isPublicDeepSeekImageIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range deepSeekBlockedPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func newDeepSeekImageHTTPClient(proxied, serverName string) (*http.Client, *http.Transport, error) {
	dialer := &net.Dialer{Timeout: deepSeekImageHeaderTimeout, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName},
		TLSHandshakeTimeout:   deepSeekImageHeaderTimeout,
		ResponseHeaderTimeout: deepSeekImageHeaderTimeout,
		IdleConnTimeout:       30 * time.Second,
		DisableKeepAlives:     true,
	}

	if strings.TrimSpace(proxied) != "" {
		proxyURL, err := url.Parse(proxied)
		if err != nil {
			return nil, nil, errors.New("invalid image proxy URL")
		}
		switch strings.ToLower(proxyURL.Scheme) {
		case "http", "https":
			transport.Proxy = http.ProxyURL(proxyURL)
		case "socks5", "socks5h":
			if strings.EqualFold(proxyURL.Scheme, "socks5h") {
				proxyURL.Scheme = "socks5"
			}
			proxyDialer, err := xproxy.FromURL(proxyURL, dialer)
			if err != nil {
				return nil, nil, fmt.Errorf("configure image proxy: %w", err)
			}
			contextDialer, ok := proxyDialer.(xproxy.ContextDialer)
			if !ok {
				return nil, nil, errors.New("configured image proxy does not support context cancellation")
			}
			transport.DialContext = contextDialer.DialContext
		default:
			return nil, nil, errors.New("image proxy must use http, https, socks5, or socks5h")
		}
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client, transport, nil
}

func isDeepSeekRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}
