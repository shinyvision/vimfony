package analyzer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestYAMLAnalyzerOnDefinition(t *testing.T) {
	content := `services:
	  App\Service\Foo:
	    class: 'VendorNamespace\TestClass'
    arguments:
      - '@test.service'
  template: "template.html.twig"
`

	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	an := NewYamlAnalyzer().(*yamlAnalyzer)
	container := &config.ContainerConfig{
		WorkspaceRoot:     mockRoot,
		Roots:             []string{"."},
		BundleRoots:       make(map[string][]string),
		ServiceClasses:    map[string]string{"test.service": "VendorNamespace\\TestClass"},
		ServiceAliases:    make(map[string]string),
		ServiceReferences: make(map[string]int),
	}
	an.SetContainerConfig(container)
	autoload := config.AutoloadMap{
		PSR4: map[string][]string{
			"VendorNamespace\\": {"vendor"},
		},
	}
	an.SetAutoloadMap(&autoload)
	store := php.NewDocumentStore(10)
	store.Configure(autoload, mockRoot)
	an.SetDocumentStore(store)
	an.SetDocumentPath("/tmp/test.yaml")
	require.NoError(t, an.Changed([]byte(content), nil))

	servicePos := positionAfter(t, []byte(content), "@test.service", len("@test"))
	serviceLocs, err := an.OnDefinition(servicePos)
	require.NoError(t, err)
	require.NotEmpty(t, serviceLocs)
	expectedClassPath := filepath.Join(mockRoot, "vendor", "TestClass.php")
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedClassPath)), serviceLocs[0].URI)

	classPos := positionAfter(t, []byte(content), "VendorNamespace\\TestClass", len("VendorNamespace\\"))
	classLocs, err := an.OnDefinition(classPos)
	require.NoError(t, err)
	require.NotEmpty(t, classLocs)
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedClassPath)), classLocs[0].URI)

	twigPos := positionAfter(t, []byte(content), "template.html.twig", len("template"))
	twigLocs, err := an.OnDefinition(twigPos)
	require.NoError(t, err)
	require.NotEmpty(t, twigLocs)
	expectedTwig := filepath.Join(mockRoot, "template.html.twig")
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(expectedTwig)), twigLocs[0].URI)
}

func TestYAMLTemplateCompletion(t *testing.T) {
	content := "template: ''\nother: value\ntemplate: "

	an := NewYamlAnalyzer().(*yamlAnalyzer)

	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	container := &config.ContainerConfig{
		WorkspaceRoot:     mockRoot,
		Roots:             []string{"."},
		BundleRoots:       map[string][]string{"MyBundle": {filepath.Join(mockRoot, "bundles", "MyBundle", "views")}},
		ServiceClasses:    make(map[string]string),
		ServiceAliases:    make(map[string]string),
		ServiceReferences: make(map[string]int),
	}
	an.SetContainerConfig(container)
	require.NoError(t, an.Changed([]byte(content), nil))

	testCases := []struct {
		needle string
		offset int
		label  string
	}{
		{"template: '", len("template: '"), "quoted"},
		{"template: ", len("template: "), "unquoted"},
	}

	for _, tc := range testCases {
		pos := yamlPositionAfter(t, content, tc.needle, tc.offset)
		items, err := an.OnCompletion(pos)
		require.NoErrorf(t, err, "completion error for %s context", tc.label)
		require.NotEmptyf(t, items, "expected completion items for %s context", tc.label)

		labels := make([]string, 0, len(items))
		for _, item := range items {
			labels = append(labels, item.Label)
		}

		require.Containsf(t, labels, "template.html.twig", "expected base template suggestion for %s context", tc.label)
		require.Containsf(t, labels, "@MyBundle/example.html.twig", "expected bundle template suggestion for %s context", tc.label)
	}
}

func yamlPositionAfter(t *testing.T, content, needle string, offset int) protocol.Position {
	idx := strings.Index(content, needle)
	require.NotEqualf(t, -1, idx, "needle %q not found", needle)

	target := idx + offset
	require.GreaterOrEqual(t, target, 0)
	require.LessOrEqual(t, target, len(content))

	line := strings.Count(content[:target], "\n")
	col := target
	if last := strings.LastIndex(content[:target], "\n"); last >= 0 {
		col = target - last - 1
	}

	return protocol.Position{
		Line:      uint32(line),
		Character: uint32(col),
	}
}
