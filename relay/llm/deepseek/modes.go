package deepseek

import "strings"

type deepSeekMode struct {
	ModelType       string
	ThinkingEnabled bool
	SearchEnabled   bool
	VisionEnabled   bool
}

var deepSeekModelOrder = []string{
	"deepseek-chat",
	"deepseek-reasoner",
	"deepseek-v4-flash",
	"deepseek-v4-flash-nothinking",
	"deepseek-v4-pro",
	"deepseek-v4-pro-nothinking",
	"deepseek-v4-flash-search",
	"deepseek-v4-flash-search-nothinking",
	"deepseek-v4-vision",
	"deepseek-v4-vision-nothinking",
}

func resolveDeepSeekMode(model string) (deepSeekMode, bool) {
	model = strings.ToLower(strings.TrimSpace(model))

	// Keep the legacy aliases stable for existing clients.
	switch model {
	case "deepseek-chat":
		return deepSeekMode{ModelType: "default"}, true
	case "deepseek-reasoner":
		return deepSeekMode{ModelType: "default", ThinkingEnabled: true}, true
	}

	noThinking := strings.HasSuffix(model, "-nothinking")
	baseModel := strings.TrimSuffix(model, "-nothinking")
	mode := deepSeekMode{ThinkingEnabled: !noThinking}

	switch baseModel {
	case "deepseek-v4-flash":
		mode.ModelType = "default"
	case "deepseek-v4-pro":
		mode.ModelType = "expert"
	case "deepseek-v4-flash-search":
		mode.ModelType = "default"
		mode.SearchEnabled = true
	case "deepseek-v4-pro-search":
		// The current website hides search in expert mode. Keep these aliases
		// request-compatible, but do not advertise them in Models().
		mode.ModelType = "expert"
		mode.SearchEnabled = true
	case "deepseek-v4-vision":
		mode.ModelType = "vision"
		mode.VisionEnabled = true
	default:
		return deepSeekMode{}, false
	}

	return mode, true
}
