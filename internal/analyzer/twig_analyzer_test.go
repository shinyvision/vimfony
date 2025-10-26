package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
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

func TestTwigDefinitionForIncludePath(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "target.twig")
	require.NoError(t, os.WriteFile(targetPath, []byte("{# stub #}"), 0o644))

	content := "{{ include(\"target.twig\") }}"
	an := NewTwigAnalyzer().(*twigAnalyzer)

	container := &config.ContainerConfig{
		WorkspaceRoot: tmpDir,
		Roots:         []string{tmpDir},
		BundleRoots:   make(map[string][]string),
		TwigFunctions: make(map[string]protocol.Location),
	}
	an.SetContainerConfig(container)
	require.NoError(t, an.Changed([]byte(content), nil))

	offset := strings.Index(content, "target.twig") + 3
	pos := protocol.Position{Line: 0, Character: uint32(offset)}

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(targetPath)), locs[0].URI)
}

func TestTwigDefinitionForRegisteredFunction(t *testing.T) {
	content := "{{ my_function(variable) }}"
	an := NewTwigAnalyzer().(*twigAnalyzer)

	container := &config.ContainerConfig{
		TwigFunctions: map[string]protocol.Location{
			"my_function": {
				URI:   "file:///tmp/mock.php",
				Range: protocol.Range{Start: protocol.Position{Line: 10}, End: protocol.Position{Line: 10, Character: 5}},
			},
		},
	}
	an.SetContainerConfig(container)
	require.NoError(t, an.Changed([]byte(content), nil))

	offset := strings.Index(content, "my_function") + 2
	pos := protocol.Position{Line: 0, Character: uint32(offset)}

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.Len(t, locs, 1)
	require.Equal(t, container.TwigFunctions["my_function"], locs[0])
}

func TestTwigDefinitionForRouteControllerAction(t *testing.T) {
	content := "{{ path('a_route') }}"
	an := NewTwigAnalyzer().(*twigAnalyzer)

	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	container := &config.ContainerConfig{
		WorkspaceRoot:     mockRoot,
		ServiceClasses:    make(map[string]string),
		ServiceAliases:    make(map[string]string),
		ServiceReferences: make(map[string]int),
	}
	an.SetContainerConfig(container)
	psr4 := config.Psr4Map{
		"VendorNamespace\\": []string{"vendor"},
	}
	an.SetPsr4Map(&psr4)
	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Controller: "VendorNamespace\\TestClass",
			Action:     "index",
		},
	}
	an.SetRoutes(&routes)
	require.NoError(t, an.Changed([]byte(content), nil))

	start := strings.Index(content, "a_route")
	require.NotEqual(t, -1, start)
	idx := start + 2
	pos := protocol.Position{Line: 0, Character: uint32(idx)}

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)

	expectedPath := filepath.Join(mockRoot, "vendor", "TestClass.php")
	expectedRange, ok := php.FindMethodRange(expectedPath, "index")
	require.True(t, ok)
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedPath)), locs[0].URI)
	require.Equal(t, expectedRange, locs[0].Range)
}

func TestTwigDefinitionForRouteControllerInvokeFallback(t *testing.T) {
	content := "{{ url('a_route') }}"
	an := NewTwigAnalyzer().(*twigAnalyzer)

	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	container := &config.ContainerConfig{
		WorkspaceRoot: mockRoot,
		ServiceClasses: map[string]string{
			"test.controller": "VendorNamespace\\TestClass",
		},
		ServiceAliases:    make(map[string]string),
		ServiceReferences: make(map[string]int),
	}
	an.SetContainerConfig(container)
	psr4 := config.Psr4Map{
		"VendorNamespace\\": []string{"vendor"},
	}
	an.SetPsr4Map(&psr4)
	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Controller: "test.controller",
			Action:     "missingAction",
		},
	}
	an.SetRoutes(&routes)
	require.NoError(t, an.Changed([]byte(content), nil))

	start := strings.Index(content, "a_route")
	require.NotEqual(t, -1, start)
	idx := start + 2
	pos := protocol.Position{Line: 0, Character: uint32(idx)}

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)

	expectedPath := filepath.Join(mockRoot, "vendor", "TestClass.php")
	invokeRange, ok := php.FindMethodRange(expectedPath, "__invoke")
	require.True(t, ok)
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedPath)), locs[0].URI)
	require.Equal(t, invokeRange, locs[0].Range)
}
