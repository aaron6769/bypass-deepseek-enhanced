package toolcall

import (
	"chatgpt-adapter/core/common/vars"
	"chatgpt-adapter/core/gin/model"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNeedExecUsesStandardToolsByDefault(t *testing.T) {
	tests := []struct {
		name       string
		toolChoice interface{}
		configure  model.Keyv[interface{}]
		want       bool
	}{
		{name: "standard tools default to enabled", toolChoice: "auto", want: true},
		{name: "tool choice none disables tools", toolChoice: "none", want: false},
		{
			name: "function tool choice object remains enabled",
			toolChoice: map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": "get_weather",
				},
			},
			want: true,
		},
		{
			name:       "explicit internal disable is preserved",
			toolChoice: "auto",
			configure: model.Keyv[interface{}]{
				"id": "-1", "enabled": false, "tasks": false,
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set(vars.GinCompletion, model.Completion{
				Messages: []model.Keyv[interface{}]{{"role": "user", "content": "use a tool"}},
				Tools: []model.Keyv[interface{}]{{
					"type": "function",
					"function": map[string]interface{}{
						"name": "get_weather",
					},
				}},
				ToolChoice: test.toolChoice,
			})
			if test.configure != nil {
				ctx.Set(vars.GinTool, test.configure)
			}

			if got := NeedExec(ctx); got != test.want {
				t.Fatalf("NeedExec() = %v, want %v", got, test.want)
			}
		})
	}
}
