package provider

func DefaultBaseURL(backend string) string {
	switch backend {
	case "lmstudio":
		return lmStudioDefaultBaseURL
	case "openrouter":
		return openRouterDefaultBaseURL
	case "codex":
		return codexDefaultBaseURL
	default:
		return ""
	}
}
