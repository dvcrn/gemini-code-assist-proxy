package server

import "testing"

func TestNormalizeModelName(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "gemini-3-pro-preview",
			input:    "gemini-3-pro-preview",
			expected: "gemini-3-pro-preview",
		},
		{
			name:     "gemini-3-pro",
			input:    "gemini-3-pro",
			expected: "gemini-3-pro",
		},
		{
			name:     "alias 3-pro",
			input:    "3-pro",
			expected: "gemini-3-pro",
		},
		{
			name:     "gemini-3-flash-preview",
			input:    "gemini-3-flash-preview",
			expected: "gemini-3-flash-preview",
		},
		{
			name:     "gemini-3-flash",
			input:    "gemini-3-flash",
			expected: "gemini-3-flash",
		},
		{
			name:     "alias 3-flash",
			input:    "3-flash",
			expected: "gemini-3-flash",
		},
		{
			name:     "alias with slash",
			input:    "models/gemini-3-pro-preview",
			expected: "gemini-3-pro-preview",
		},
		{
			name:     "gemini-2.5-pro",
			input:    "gemini-2.5-pro",
			expected: "gemini-2.5-pro",
		},
		{
			name:     "alias gemini-pro (defaults to 3-pro)",
			input:    "gemini-pro",
			expected: "gemini-3-pro",
		},
		{
			name:     "gemini-2.5-flash",
			input:    "gemini-2.5-flash",
			expected: "gemini-2.5-flash",
		},
		{
			name:     "alias gemini-flash (defaults to 3-flash)",
			input:    "gemini-flash",
			expected: "gemini-3-flash",
		},
		{
			name:     "gemini-2.5-flash-lite",
			input:    "gemini-lite",
			expected: "gemini-2.5-flash-lite",
		},
		{
			name:     "unknown model",
			input:    "unknown-model",
			expected: "unknown-model",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			normalized := normalizeModelName(tc.input)
			if normalized != tc.expected {
				t.Errorf("Expected model name %s, but got %s", tc.expected, normalized)
			}
		})
	}
}
