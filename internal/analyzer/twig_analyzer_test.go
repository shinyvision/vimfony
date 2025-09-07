package analyzer

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestIsTypingFunction(t *testing.T) {
	content, err := os.ReadFile("../../mock/template.html.twig")
	require.NoError(t, err)

	testCases := []struct {
		name           string
		pos            protocol.Position
		expectedFound  bool
		expectedPrefix string
	}{
		{
			name:           "variable_1",
			pos:            protocol.Position{Line: 1, Character: 6},
			expectedFound:  true,
			expectedPrefix: "var",
		},
		{
			name:           "variable_2",
			pos:            protocol.Position{Line: 3, Character: 10},
			expectedFound:  true,
			expectedPrefix: "varia",
		},
		{
			name:           "not_a_variable",
			pos:            protocol.Position{Line: 3, Character: 17},
			expectedFound:  false,
			expectedPrefix: "",
		},
		{
			name:           "function_identifier",
			pos:            protocol.Position{Line: 4, Character: 9},
			expectedFound:  true,
			expectedPrefix: "call",
		},
	}

	analyzer := NewTwigAnalyzer().(*twigAnalyzer)
	analyzer.Changed([]byte(content), nil)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, prefix := analyzer.IsTypingFunction(tc.pos)
			assert.Equal(t, tc.expectedFound, found)
			assert.Equal(t, tc.expectedPrefix, prefix)
		})
	}
}
