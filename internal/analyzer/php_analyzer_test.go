package analyzer

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestIsInAutoconfigure(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_autoconfigure.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	analyzer.Changed(content, nil)

	// Test case 1: Inside autoconfigure
	pos1 := protocol.Position{Line: 11, Character: 23}
	found1, msg1 := analyzer.(*phpAnalyzer).isInAutoconfigure(pos1)
	require.True(t, found1, "Test case 1 failed: %s", msg1)

	// Test case 2: Outside autoconfigure
	pos2 := protocol.Position{Line: 20, Character: 14}
	found2, _ := analyzer.(*phpAnalyzer).isInAutoconfigure(pos2)
	require.False(t, found2)
}

func BenchmarkIsInAutoconfigure(b *testing.B) {
	content, err := os.ReadFile("../../mock/class_with_autoconfigure.php")
	if err != nil {
		b.Fatalf("failed to read mock PHP file: %v", err)
	}

	analyzer := NewPHPAnalyzer()
	analyzer.Changed(content, nil)
	pos := protocol.Position{Line: 11, Character: 23}

	b.ReportAllocs()

	for b.Loop() {
		_, _ = analyzer.(*phpAnalyzer).isInAutoconfigure(pos)
	}
}

func TestPHPPropertyTypeCollection(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	require.NoError(t, analyzer.Changed(content, nil))

	pa := analyzer.(*phpAnalyzer)
	types := pa.propertyTypes

	expected := map[string][]string{
		"urlGenerator":          {urlGeneratorFQN},
		"urlGeneratorInterface": {urlGeneratorInterfaceFQN},
		"notARouter":            {"App\\ThisIsNotARouter"},
		"myAliasedRouter":       {routerInterfaceFQN},
		"router":                {routerFQN},
		"routerInterface":       {routerInterfaceFQN},
		"fqnRouter":             {routerInterfaceFQN},
	}

	require.Len(t, types, len(expected))

	for name, want := range expected {
		got, ok := types[name]
		require.Truef(t, ok, "missing property %s", name)
		gotTypes := make([]string, 0, len(got))
		for _, occ := range got {
			gotTypes = append(gotTypes, occ.Type)
			require.Greater(t, occ.Line, 0)
		}
		require.ElementsMatchf(t, want, gotTypes, "property %s type mismatch", name)
	}
}

func TestPHPRouterRouteNameCompletion(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	require.NoError(t, analyzer.Changed(content, nil))

	pa := analyzer.(*phpAnalyzer)

	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Parameters: []string{"some", "unborn_param_name"},
		},
		"another_route": {
			Name:       "another_route",
			Parameters: []string{"foo"},
		},
	}
	pa.SetRoutes(routes)

	target := "$this->router->generate('a_route'"
	offset := strings.Index(target, "'a_route'") + 1
	pos := positionAfter(t, content, target, offset)

	items, err := pa.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "a_route")
	require.Contains(t, labels, "another_route")
}

func TestPHPRouterRouteCompletionForAssignedVariable(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	require.NoError(t, analyzer.Changed(content, nil))

	pa := analyzer.(*phpAnalyzer)

	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Parameters: []string{"some", "unborn_param_name"},
		},
	}
	pa.SetRoutes(routes)

	target := "$i = $assignedRouterToVariable->generate('a_route', ['some' => 'params'])"
	offset := strings.Index(target, "'a_route'") + 1
	pos := positionAfter(t, content, target, offset)

	items, err := pa.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "a_route")
}

func TestPHPRouterRouteCompletionForDocblockVariable(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	require.NoError(t, analyzer.Changed(content, nil))

	pa := analyzer.(*phpAnalyzer)

	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Parameters: []string{"some"},
		},
	}
	pa.SetRoutes(routes)

	target := "$j = $typeHintedRouter->generate('a_route', ['some' => 'params'])"
	offset := strings.Index(target, "'a_route'") + 1
	pos := positionAfter(t, content, target, offset)

	items, err := pa.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "a_route")
}

func TestPHPRouterRouteParameterCompletion(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	require.NoError(t, analyzer.Changed(content, nil))

	pa := analyzer.(*phpAnalyzer)

	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Parameters: []string{"some", "unborn_param_name"},
		},
	}
	pa.SetRoutes(routes)

	target := "$this->router->generate('a_route', ['some' => 'params'])"
	offset := strings.Index(target, "['some'") + len("['")
	pos := positionAfter(t, content, target, offset)

	items, err := pa.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "some")
	require.Contains(t, labels, "unborn_param_name")
}

func TestPHPRouterRouteParameterCompletionWithoutArrow(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	require.NoError(t, analyzer.Changed(content, nil))

	pa := analyzer.(*phpAnalyzer)

	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Parameters: []string{"some", "unborn_param_name"},
		},
	}
	pa.SetRoutes(routes)

	target := "$this->router->generate('a_route', ['unborn_param_name'])"
	offset := strings.Index(target, "['unborn_param_name") + len("['")
	pos := positionAfter(t, content, target, offset)

	items, err := pa.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "some")
	require.Contains(t, labels, "unborn_param_name")
}

func TestPHPRouterRouteCompletionNotOfferedForNonRouter(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	require.NoError(t, analyzer.Changed(content, nil))

	pa := analyzer.(*phpAnalyzer)

	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Parameters: []string{"some"},
		},
	}
	pa.SetRoutes(routes)

	target := "$this->notARouter->generate('generating_something_that_is_not_a_route')"
	offset := strings.Index(target, "('generating_something_that_is_not_a_route") + len("('")
	pos := positionAfter(t, content, target, offset)

	items, err := pa.OnCompletion(pos)
	require.NoError(t, err)
	require.Nil(t, items)
}

func positionAfter(t *testing.T, content []byte, needle string, offset int) protocol.Position {
	idx := bytes.Index(content, []byte(needle))
	require.NotEqualf(t, -1, idx, "needle %q not found", needle)

	target := idx + offset
	require.GreaterOrEqual(t, target, 0)
	require.LessOrEqual(t, target, len(content))

	line := bytes.Count(content[:target], []byte("\n"))
	col := target
	if last := bytes.LastIndex(content[:target], []byte("\n")); last >= 0 {
		col = target - last - 1
	}

	return protocol.Position{
		Line:      uint32(line),
		Character: uint32(col),
	}
}
