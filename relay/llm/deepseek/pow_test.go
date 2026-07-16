package deepseek

import (
	"chatgpt-adapter/core/gin/model"
	"context"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

func TestDeepSeekHashV1Vectors(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "e594808bc5b7151ac160c6d39a02e0a8e261ed588578403099e3561dc40c26b3"},
		{"testsalt_1700000000_42", "d4a2ea58c89e40887c933484868380c6f803eaa8dc53a3b9df8e431b921a4f09"},
		{"testsalt_1700000000_100000", "abea2f35796b65486e9be1b36f7878c66cab021e96faa473fdf4decd31f9ba30"},
		{"abc123salt_1700000000_12345", "74b3b7452745b70e85eb32ee7f0a9ec0381d42dd5137b695da915e104fc390e1"},
	}
	for _, test := range tests {
		got := deepSeekHashV1([]byte(test.input))
		if hex.EncodeToString(got[:]) != test.want {
			t.Errorf("hash(%q) = %s, want %s", test.input, hex.EncodeToString(got[:]), test.want)
		}
	}
}

func TestEncodeDeepSeekPoWPayloadUsesBrowserCompatibleBase64(t *testing.T) {
	if got := encodeDeepSeekPoWPayload([]byte("a")); got != "YQ==" {
		t.Fatalf("encodeDeepSeekPoWPayload() = %q, want padded browser-compatible base64", got)
	}
}

func TestLocalPOWSolverWithRealChallenge(t *testing.T) {
	answer, err := calcAnswer(context.Background(), map[string]interface{}{
		"challenge":  "476138e5d25811cae449a34bbcae224c1a0df4f0933942a9191b42ac1017837e",
		"salt":       "a3e76e9270ce16d56035",
		"difficulty": float64(144000),
		"expire_at":  float64(1783934503381),
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer != 71079 {
		t.Fatalf("answer = %d, want 71079", answer)
	}
}

func BenchmarkLocalPOWSolverWithRealChallenge(b *testing.B) {
	for i := 0; i < b.N; i++ {
		answer, err := solveDeepSeekPOW(
			context.Background(),
			"476138e5d25811cae449a34bbcae224c1a0df4f0933942a9191b42ac1017837e",
			"a3e76e9270ce16d56035",
			1783934503381,
			144000,
		)
		if err != nil || answer != 71079 {
			b.Fatalf("answer = %d, err = %v", answer, err)
		}
	}
}

func TestListDeepSeekModels(t *testing.T) {
	models := (&api{}).Models()
	if len(models) != len(deepSeekModelOrder) {
		t.Fatalf("DeepSeek adapter returned %d models, want %d", len(models), len(deepSeekModelOrder))
	}
	for index, item := range models {
		if item.Id != deepSeekModelOrder[index] {
			t.Fatalf("model[%d] = %q, want %q", index, item.Id, deepSeekModelOrder[index])
		}
	}
}

func TestResolveDeepSeekModes(t *testing.T) {
	tests := map[string]deepSeekMode{
		"deepseek-chat":                     {ModelType: "default"},
		"deepseek-reasoner":                 {ModelType: "default", ThinkingEnabled: true},
		"deepseek-v4-flash":                 {ModelType: "default", ThinkingEnabled: true},
		"deepseek-v4-flash-nothinking":      {ModelType: "default"},
		"deepseek-v4-pro-search":            {ModelType: "expert", ThinkingEnabled: true, SearchEnabled: true},
		"deepseek-v4-pro-search-nothinking": {ModelType: "expert", SearchEnabled: true},
		"deepseek-v4-vision":                {ModelType: "vision", ThinkingEnabled: true, VisionEnabled: true},
		"deepseek-v4-vision-nothinking":     {ModelType: "vision", VisionEnabled: true},
	}
	for modelID, want := range tests {
		got, ok := resolveDeepSeekMode(modelID)
		if !ok {
			t.Fatalf("resolveDeepSeekMode(%q) was not matched", modelID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("resolveDeepSeekMode(%q) = %#v, want %#v", modelID, got, want)
		}
	}
	if _, ok := resolveDeepSeekMode("deepseek-v4-vision-search"); ok {
		t.Fatal("unsupported vision-search mode was matched")
	}
}

func TestDeepSeekMessageContent(t *testing.T) {
	message := model.Keyv[interface{}]{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "describe this"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,aGVsbG8="}},
		},
	}
	text, imageURLs := deepSeekMessageContent(message)
	if text != "describe this" {
		t.Fatalf("text = %q", text)
	}
	if len(imageURLs) != 1 || imageURLs[0] != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("imageURLs = %#v", imageURLs)
	}
}

func TestLoadDeepSeekDataImage(t *testing.T) {
	data, contentType, filename, err := loadDeepSeekImage(context.Background(), "", "data:image/png;base64,aGVsbG8=")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" || contentType != "image/png" || filename != "image.png" {
		t.Fatalf("data=%q contentType=%q filename=%q", string(data), contentType, filename)
	}
}

func TestExtractDeepSeekFile(t *testing.T) {
	fileID, status := extractDeepSeekFile(map[string]interface{}{
		"data": map[string]interface{}{
			"biz_data": map[string]interface{}{
				"file": map[string]interface{}{"id": "file-123", "status": "ready"},
			},
		},
	}, "file-123")
	if fileID != "file-123" || status != "ready" {
		t.Fatalf("fileID=%q status=%q", fileID, status)
	}
}

func TestDeepSeekRequestModeFields(t *testing.T) {
	payload, err := json.Marshal(deepseekRequest{
		ChatSessionId:   "session-1",
		ModelType:       "expert",
		RefFileIds:      []string{},
		ThinkingEnabled: false,
		SearchEnabled:   true,
		Message:         "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["model_type"] != "expert" || decoded["thinking_enabled"] != false || decoded["search_enabled"] != true {
		t.Fatalf("unexpected mode payload: %s", payload)
	}
}

func TestExtractDeepSeekSessionID(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{
			name: "legacy top-level id",
			data: map[string]interface{}{"id": "session-legacy"},
			want: "session-legacy",
		},
		{
			name: "current nested chat session id",
			data: map[string]interface{}{
				"chat_session": map[string]interface{}{"id": "session-current"},
			},
			want: "session-current",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractDeepSeekSessionID(test.data); got != test.want {
				t.Fatalf("extractDeepSeekSessionID() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIsDeepSeekContentPatchPath(t *testing.T) {
	tests := map[string]bool{
		"response/content":                       true,
		"response/fragments/-1/content":          true,
		"response/fragments/0/content":           true,
		"response/fragments/-1/thinking_content": false,
		"response/status":                        false,
	}
	for path, want := range tests {
		if got := isDeepSeekContentPatchPath(path); got != want {
			t.Fatalf("isDeepSeekContentPatchPath(%q) = %v, want %v", path, got, want)
		}
	}
}
