package analyzer

import (
	"path/filepath"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
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
	psr4 := config.Psr4Map{
		"VendorNamespace\\": []string{"vendor"},
	}
	an.SetPsr4Map(&psr4)
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
