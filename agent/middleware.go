package agent

func checkCompaction(session *Session, threshold int) bool {
	return session.PromptTokens+session.CompletionTokens > threshold
}
