package analyzer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
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
	idx := pa.indexSnapshot()
	types := idx.Properties

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
			Controller: "",
		},
		"another_route": {
			Name:       "another_route",
			Parameters: []string{"foo"},
			Controller: "",
		},
	}
	pa.SetRoutes(&routes)

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
			Controller: "",
		},
	}
	pa.SetRoutes(&routes)

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
			Controller: "",
		},
	}
	pa.SetRoutes(&routes)

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

func TestPHPDefinitionForClassReference(t *testing.T) {
	content := "<?php\n$cls = VendorNamespace\\TestClass::class;\n"

	an := NewPHPAnalyzer().(*phpAnalyzer)

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
	store := php.NewDocumentStore(10)
	store.Configure(psr4, mockRoot)
	an.SetDocumentStore(store)
	an.SetPsr4Map(&psr4)

	require.NoError(t, an.Changed([]byte(content), nil))

	classRef := "VendorNamespace\\TestClass"
	pos := positionAfter(t, []byte(content), classRef, len(classRef)/2)

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)

	expectedPath := filepath.Join(mockRoot, "vendor", "TestClass.php")
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedPath)), locs[0].URI)
}

func TestPHPDefinitionForServiceID(t *testing.T) {
	content := "<?php\n$service = 'test.service';\n"

	an := NewPHPAnalyzer().(*phpAnalyzer)

	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	container := &config.ContainerConfig{
		WorkspaceRoot:     mockRoot,
		ServiceClasses:    map[string]string{"test.service": "VendorNamespace\\TestClass"},
		ServiceAliases:    make(map[string]string),
		ServiceReferences: make(map[string]int),
	}
	an.SetContainerConfig(container)
	psr4 := config.Psr4Map{
		"VendorNamespace\\": []string{"vendor"},
	}
	store := php.NewDocumentStore(10)
	store.Configure(psr4, mockRoot)
	an.SetDocumentStore(store)
	an.SetPsr4Map(&psr4)

	require.NoError(t, an.Changed([]byte(content), nil))

	serviceRef := "test.service"
	pos := positionAfter(t, []byte(content), serviceRef, len(serviceRef)/2)

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)

	expectedPath := filepath.Join(mockRoot, "vendor", "TestClass.php")
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedPath)), locs[0].URI)
}

func TestPHPDefinitionForRouteControllerAction(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	an := NewPHPAnalyzer().(*phpAnalyzer)

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
	store := php.NewDocumentStore(10)
	store.Configure(psr4, mockRoot)
	an.SetDocumentStore(store)
	an.SetPsr4Map(&psr4)
	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Parameters: []string{"some"},
			Controller: "VendorNamespace\\TestClass",
			Action:     "index",
		},
	}
	an.SetRoutes(&routes)
	path, _, ok := php.Resolve("VendorNamespace\\TestClass", psr4, container.WorkspaceRoot)
	if !ok {
		t.Fatalf("php.Resolve failed (root=%s map=%v)", container.WorkspaceRoot, psr4)
	}
	_, err = store.Get(path)
	require.NoError(t, err)
	doc, uri, ok := routeDocument(routes["a_route"], container, psr4, store)
	require.True(t, ok)
	require.NotEmpty(t, resolveRouteLocations(routes["a_route"], uri, doc))

	require.NoError(t, an.Changed(content, nil))

	target := "$this->router->generate('a_route'"
	offset := strings.Index(target, "'a_route'") + 1
	pos := positionAfter(t, content, target, offset)

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)

	expectedPath := filepath.Join(mockRoot, "vendor", "TestClass.php")
	expectedRange, ok := php.FindMethodRange(expectedPath, "index")
	require.True(t, ok)
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedPath)), locs[0].URI)
	require.Equal(t, expectedRange, locs[0].Range)
}

func TestPHPDefinitionForRouteControllerInvokeFallback(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_router.php")
	require.NoError(t, err)

	an := NewPHPAnalyzer().(*phpAnalyzer)

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
	store := php.NewDocumentStore(10)
	store.Configure(psr4, mockRoot)
	an.SetDocumentStore(store)
	an.SetPsr4Map(&psr4)
	routes := config.RoutesMap{
		"a_route": {
			Name:       "a_route",
			Parameters: []string{"some"},
			Controller: "test.controller",
			Action:     "missingAction",
		},
	}
	an.SetRoutes(&routes)
	path, _, ok := php.Resolve("VendorNamespace\\TestClass", psr4, container.WorkspaceRoot)
	require.True(t, ok, "expected php.Resolve to succeed")
	_, err = store.Get(path)
	require.NoError(t, err)
	doc, uri, ok := routeDocument(routes["a_route"], container, psr4, store)
	require.True(t, ok)
	require.NotEmpty(t, resolveRouteLocations(routes["a_route"], uri, doc))

	require.NoError(t, an.Changed(content, nil))

	target := "$this->router->generate('a_route'"
	offset := strings.Index(target, "'a_route'") + 1
	pos := positionAfter(t, content, target, offset)

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)

	expectedPath := filepath.Join(mockRoot, "vendor", "TestClass.php")
	invokeRange, ok := php.FindMethodRange(expectedPath, "__invoke")
	require.True(t, ok)
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedPath)), locs[0].URI)
	require.Equal(t, invokeRange, locs[0].Range)
}

func TestPHPRouterCompletionForAbstractControllerHelpers(t *testing.T) {
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
	pa.SetRoutes(&routes)

	targetGenerate := "$k = $this->generateUrl('a_route', ['some' => 'params']);"
	offsetGenerate := strings.Index(targetGenerate, "'a_route'") + 1
	posGenerate := positionAfter(t, content, targetGenerate, offsetGenerate)

	itemsGenerate, err := pa.OnCompletion(posGenerate)
	require.NoError(t, err)
	require.NotEmpty(t, itemsGenerate)

	targetRedirect := "$i = $this->redirectToRoute('a_route', ['some' => 'params']);"
	offsetRedirect := strings.Index(targetRedirect, "'a_route'") + 1
	posRedirect := positionAfter(t, content, targetRedirect, offsetRedirect)

	itemsRedirect, err := pa.OnCompletion(posRedirect)
	require.NoError(t, err)
	require.NotEmpty(t, itemsRedirect)

	var labels []string
	for _, item := range append(itemsGenerate, itemsRedirect...) {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "a_route")
	require.Contains(t, labels, "another_route")
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
			Controller: "",
		},
	}
	pa.SetRoutes(&routes)

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
	pa.SetRoutes(&routes)

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
			Controller: "",
		},
	}
	pa.SetRoutes(&routes)

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
