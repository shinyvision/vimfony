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
			found, prefix := analyzer.isTypingFunction(tc.pos)
			assert.Equal(t, tc.expectedFound, found)
			assert.Equal(t, tc.expectedPrefix, prefix)
		})
	}
}

func TestIsTypingRouteName(t *testing.T) {
	content, err := os.ReadFile("../../mock/template.html.twig")
	require.NoError(t, err)

	testCases := []struct {
		name           string
		pos            protocol.Position
		expectedFound  bool
		expectedPrefix string
	}{
		{
			name:           "route_name_at_a",
			pos:            protocol.Position{Line: 6, Character: 11},
			expectedFound:  true,
			expectedPrefix: "",
		},
		{
			name:           "route_name_after_first_p",
			pos:            protocol.Position{Line: 6, Character: 13},
			expectedFound:  true,
			expectedPrefix: "ap",
		},
		{
			name:           "route_name_at_closing_quote",
			pos:            protocol.Position{Line: 6, Character: 17},
			expectedFound:  true,
			expectedPrefix: "app_fo",
		},
		{
			name:           "not_in_route",
			pos:            protocol.Position{Line: 1, Character: 6},
			expectedFound:  false,
			expectedPrefix: "",
		},
	}

	analyzer := NewTwigAnalyzer().(*twigAnalyzer)
	analyzer.Changed([]byte(content), nil)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, prefix := analyzer.isTypingRouteName(tc.pos)
			assert.Equal(t, tc.expectedFound, found)
			assert.Equal(t, tc.expectedPrefix, prefix)
		})
	}
}

func TestIsTypingRouteParameter(t *testing.T) {
	content, err := os.ReadFile("../../mock/template.html.twig")
	require.NoError(t, err)

	testCases := []struct {
		name              string
		pos               protocol.Position
		expectedFound     bool
		expectedRouteName string
		expectedPrefix    string
	}{
		{
			name:              "parameter_key_at_i",
			pos:               protocol.Position{Line: 6, Character: 22},
			expectedFound:     true,
			expectedRouteName: "app_fo",
			expectedPrefix:    "",
		},
		{
			name:              "parameter_key_after_i",
			pos:               protocol.Position{Line: 6, Character: 23},
			expectedFound:     true,
			expectedRouteName: "app_fo",
			expectedPrefix:    "i",
		},
		{
			name:              "unborn_key_at_i",
			pos:               protocol.Position{Line: 7, Character: 22},
			expectedFound:     true,
			expectedRouteName: "app_fo",
			expectedPrefix:    "",
		},
		{
			name:              "unborn_key_after_i",
			pos:               protocol.Position{Line: 7, Character: 23},
			expectedFound:     true,
			expectedRouteName: "app_fo",
			expectedPrefix:    "i",
		},
		{
			name:              "not_in_parameter",
			pos:               protocol.Position{Line: 1, Character: 6},
			expectedFound:     false,
			expectedRouteName: "",
			expectedPrefix:    "",
		},
	}

	analyzer := NewTwigAnalyzer().(*twigAnalyzer)
	analyzer.Changed([]byte(content), nil)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, routeName, prefix := analyzer.isTypingRouteParameter(tc.pos)
			assert.Equal(t, tc.expectedFound, found)
			assert.Equal(t, tc.expectedRouteName, routeName)
			assert.Equal(t, tc.expectedPrefix, prefix)
		})
	}
}
