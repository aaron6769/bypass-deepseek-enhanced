package gin

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDumpRequestRedacted(t *testing.T) {
	request := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test"}`))
	request.Header.Set("Authorization", "Bearer secret-token")
	request.Header.Set("X-Api-Key", "secret-api-key")
	request.Header.Set("Cookie", "session=secret-cookie")

	dump, err := dumpRequestRedacted(request, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(dump)
	for _, secret := range []string{"secret-token", "secret-api-key", "secret-cookie"} {
		if strings.Contains(text, secret) {
			t.Fatalf("request dump leaked %q: %s", secret, text)
		}
	}
	if strings.Count(text, "[redacted]") != 3 {
		t.Fatalf("expected three redacted headers, got: %s", text)
	}
}
