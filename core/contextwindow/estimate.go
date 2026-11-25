package contextwindow

const bytesPerToken = 4

// EstimateTokens estimates the tokens text takes in a context window, at about
// four bytes per token. It is a size the host announces as an estimate, never a
// count a vendor reported.
func EstimateTokens(text string) int {
	return (len(text) + bytesPerToken - 1) / bytesPerToken
}
