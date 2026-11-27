package contextwindow

import "testing"

func TestEstimateTokensRoundsUpAtFourBytesPerToken(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{text: "", want: 0},
		{text: "a", want: 1},
		{text: "abcd", want: 1},
		{text: "abcde", want: 2},
	}
	for _, tt := range tests {
		if got := EstimateTokens(tt.text); got != tt.want {
			t.Fatalf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestBytesForTokensFitsExactlyTheEstimate(t *testing.T) {
	for _, tokens := range []int{1, 2, 7, 250} {
		size := BytesForTokens(tokens)
		if got := EstimateBytes(size); got != tokens {
			t.Fatalf("EstimateBytes(BytesForTokens(%d)) = %d, want %d", tokens, got, tokens)
		}
		if got := EstimateBytes(size + 1); got != tokens+1 {
			t.Fatalf("one byte past BytesForTokens(%d) must cost one more token, got %d", tokens, got)
		}
	}
}
