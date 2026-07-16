package deepseek

import (
	"context"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestDeepSeekImageAddressPolicy(t *testing.T) {
	tests := map[string]bool{
		"1.1.1.1":              true,
		"2606:4700:4700::1111": true,
		"0.0.0.1":              false,
		"127.0.0.1":            false,
		"10.0.0.1":             false,
		"100.64.0.1":           false,
		"169.254.169.254":      false,
		"192.0.2.1":            false,
		"198.51.100.1":         false,
		"203.0.113.1":          false,
		"::1":                  false,
		"64:ff9b::a00:1":       false,
		"64:ff9b:1::a00:1":     false,
		"100::1":               false,
		"2001::1":              false,
		"2001:2::1":            false,
		"2001:db8::1":          false,
		"2002:a00:1::":         false,
		"fe80::1":              false,
	}
	for raw, want := range tests {
		address := netip.MustParseAddr(raw)
		if got := isPublicDeepSeekImageIP(address); got != want {
			t.Fatalf("isPublicDeepSeekImageIP(%s) = %v, want %v", address, got, want)
		}
	}
}

func TestDeepSeekImageProxyPolicy(t *testing.T) {
	client, transport, err := newDeepSeekImageHTTPClient("http://127.0.0.1:8080", "example.com")
	if err != nil || client == nil || transport == nil {
		t.Fatalf("HTTP proxy rejected: client=%v transport=%v err=%v", client, transport, err)
	}
	transport.CloseIdleConnections()

	if _, _, err = newDeepSeekImageHTTPClient("https://127.0.0.1:8443", "example.com"); err == nil {
		t.Fatal("HTTPS proxy was accepted without a separate proxy TLS configuration")
	}
	if _, _, err = newDeepSeekImageHTTPClient("ftp://127.0.0.1:21", "example.com"); err == nil {
		t.Fatal("unsupported image proxy scheme was accepted")
	}
}

func TestPinDeepSeekImageURLRejectsUnsafeTargets(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/image.png",
		"http://169.254.169.254/latest/meta-data/",
		"http://user:password@1.1.1.1/image.png",
		"file:///etc/passwd",
	}
	for _, raw := range tests {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err = pinDeepSeekImageURL(context.Background(), parsed); err == nil {
			t.Fatalf("pinDeepSeekImageURL(%q) accepted an unsafe URL", raw)
		}
	}
}

func TestReadDeepSeekImageBodyLimit(t *testing.T) {
	data, err := readDeepSeekImageBody(strings.NewReader("1234"), 4)
	if err != nil || string(data) != "1234" {
		t.Fatalf("within limit: data=%q err=%v", data, err)
	}
	if _, err = readDeepSeekImageBody(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("over-limit image body was accepted")
	}
}

func TestDecodeDeepSeekDataImageLimitAndType(t *testing.T) {
	data, contentType, filename, err := decodeDeepSeekDataImage("data:image/png;base64,MTIzNA==", 4)
	if err != nil || string(data) != "1234" || contentType != "image/png" || filename != "image.png" {
		t.Fatalf("data=%q contentType=%q filename=%q err=%v", data, contentType, filename, err)
	}
	if _, _, _, err = decodeDeepSeekDataImage("data:image/png;base64,MTIzNDU=", 4); err == nil {
		t.Fatal("over-limit data URI was accepted")
	}
	if _, _, _, err = decodeDeepSeekDataImage("data:text/plain;base64,MTIzNA==", 4); err == nil {
		t.Fatal("non-image data URI was accepted")
	}
}
