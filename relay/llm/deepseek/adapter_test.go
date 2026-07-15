package deepseek

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/iocgo/sdk/env"
	"github.com/spf13/viper"
)

type deepSeekTrackingBody struct {
	*bytes.Reader
	closed bool
}

func (body *deepSeekTrackingBody) Close() error {
	body.closed = true
	return nil
}

func TestDeepSeekConnectionKeepsProxyAndCookieDistinct(t *testing.T) {
	vip := viper.New()
	vip.Set("server.proxied", "http://proxy.example:8080")
	adapter := &api{env: &env.Environment{Viper: vip}}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token", "deepseek-token")

	connection := adapter.connection(ctx)
	if connection.Proxied != "http://proxy.example:8080" {
		t.Fatalf("Proxied = %q", connection.Proxied)
	}
	if connection.Cookie != "deepseek-token" {
		t.Fatalf("Cookie = %q", connection.Cookie)
	}
}

func TestCloseDeepSeekResponseDrainsAndClosesBody(t *testing.T) {
	body := &deepSeekTrackingBody{Reader: bytes.NewReader([]byte("response body"))}
	closeDeepSeekResponse(&http.Response{Body: body})
	if !body.closed {
		t.Fatal("response body was not closed")
	}
	if body.Len() != 0 {
		t.Fatalf("response body has %d unread bytes", body.Len())
	}
}
