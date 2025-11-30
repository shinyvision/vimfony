package analyzer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
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
			found, prefix := analyzer.isInServiceIDAttribute(tc.pos)
			assert.Equal(t, tc.expectedFound, found)
			assert.Equal(t, tc.expectedPrefix, prefix)
		})
	}
}

func TestXMLAnalyzerOnDefinition(t *testing.T) {
	content := `<services>
	<service id="test.service" class="VendorNamespace\TestClass" template="template.html.twig" />
	</services>`

	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	an := NewXMLAnalyzer().(*xmlAnalyzer)
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
	an.SetDocumentPath("/tmp/test.xml")
	require.NoError(t, an.Changed([]byte(content), nil))

	servicePos := positionAfter(t, []byte(content), "test.service", len("test"))
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
	require.Equal(t, protocol.DocumentUri(utils.PathToURI(filepath.Join(mockRoot, "template.html.twig"))), twigLocs[0].URI)
}
