package analyzer

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestIsInServiceIDAttribute(t *testing.T) {
	content, err := os.ReadFile("../../mock/services.xml")
	require.NoError(t, err)

	testCases := []struct {
		name           string
		pos            protocol.Position
		expectedFound  bool
		expectedPrefix string
	}{
		{
			name:           "Inside service_1, middle",
			pos:            protocol.Position{Line: 10, Character: 48},
			expectedFound:  true,
			expectedPrefix: "ser",
		},
		{
			name:           "Inside service_2, end",
			pos:            protocol.Position{Line: 11, Character: 54},
			expectedFound:  true,
			expectedPrefix: "service_2",
		},
		{
			name:           "Inside service id, not argument id",
			pos:            protocol.Position{Line: 8, Character: 30},
			expectedFound:  false,
			expectedPrefix: "",
		},
		{
			name:           "Outside any tag",
			pos:            protocol.Position{Line: 1, Character: 1},
			expectedFound:  false,
			expectedPrefix: "",
		},
		{
			name:           "Inside argument value, not id attribute",
			pos:            protocol.Position{Line: 15, Character: 25},
			expectedFound:  false,
			expectedPrefix: "",
		},
	}

	analyzer := NewXMLAnalyzer().(*xmlAnalyzer)
	analyzer.Changed([]byte(content), nil)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, prefix := analyzer.IsInServiceIDAttribute(tc.pos)
			assert.Equal(t, tc.expectedFound, found)
			assert.Equal(t, tc.expectedPrefix, prefix)
		})
	}
}
