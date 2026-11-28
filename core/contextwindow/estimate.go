package contextwindow

const bytesPerToken = 4

// EstimateTokens estimates the tokens text takes in a context window, at about
// four bytes per token. It is a size the host announces as an estimate, never a
// count a vendor reported.
func EstimateTokens(text string) int {
	return EstimateBytes(len(text))
}

// EstimateBytes estimates the tokens of a block of size bytes, at the same four bytes per token.
func EstimateBytes(size int) int {
	return (size + bytesPerToken - 1) / bytesPerToken
}

// BytesForTokens is the largest size in bytes whose estimate fits in tokens.
func BytesForTokens(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	return tokens * bytesPerToken
}
