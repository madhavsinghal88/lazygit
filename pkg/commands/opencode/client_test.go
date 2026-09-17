package opencode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPreferProvider(t *testing.T) {
	testCases := []struct {
		name        string
		providerIDs []string
		preferred   string
		expected    []string
	}{
		{
			name:        "moves preferred provider to the front",
			providerIDs: []string{"github-copilot", "opencode"},
			preferred:   "opencode",
			expected:    []string{"opencode", "github-copilot"},
		},
		{
			name:        "preserves order when preferred is already first",
			providerIDs: []string{"opencode", "github-copilot"},
			preferred:   "opencode",
			expected:    []string{"opencode", "github-copilot"},
		},
		{
			name:        "preserves order when preferred is absent",
			providerIDs: []string{"github-copilot", "anthropic"},
			preferred:   "opencode",
			expected:    []string{"github-copilot", "anthropic"},
		},
		{
			name:        "empty input",
			providerIDs: nil,
			preferred:   "opencode",
			expected:    []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, preferProvider(tc.providerIDs, tc.preferred))
		})
	}
}
