package deepseek

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestValidateDeepSeekImageURLRejectsNonPublicAddresses(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "public IPv4", rawURL: "https://8.8.8.8/image.png"},
		{name: "loopback IPv4", rawURL: "http://127.0.0.1/image.png", wantErr: true},
		{name: "loopback IPv6", rawURL: "http://[::1]/image.png", wantErr: true},
		{name: "private IPv4", rawURL: "http://10.0.0.1/image.png", wantErr: true},
		{name: "link-local metadata", rawURL: "http://169.254.169.254/latest/meta-data", wantErr: true},
		{name: "carrier-grade NAT", rawURL: "http://100.64.0.1/image.png", wantErr: true},
		{name: "NAT64 private target", rawURL: "http://[64:ff9b::a00:1]/image.png", wantErr: true},
		{name: "embedded credentials", rawURL: "https://user:pass@8.8.8.8/image.png", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := url.Parse(test.rawURL)
			if err != nil {
				t.Fatal(err)
			}
			err = validateDeepSeekImageURL(context.Background(), parsed)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateDeepSeekImageURL(%q) error = %v, wantErr %v", test.rawURL, err, test.wantErr)
			}
		})
	}
}

func TestReadDeepSeekImageRejectsDeclaredOversize(t *testing.T) {
	_, err := readDeepSeekImage(strings.NewReader("small body"), maxDeepSeekImageBytes+1)
	if err == nil {
		t.Fatal("readDeepSeekImage accepted an oversized Content-Length")
	}
}

func TestLoadDeepSeekDataImageRejectsNonImageMediaType(t *testing.T) {
	_, _, _, err := loadDeepSeekImage(context.Background(), "", "data:text/plain;base64,aGVsbG8=")
	if err == nil {
		t.Fatal("loadDeepSeekImage accepted a non-image data URI")
	}
}
