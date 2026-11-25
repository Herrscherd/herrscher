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
