package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctrineDriverExtraction(t *testing.T) {
	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	containerPath := filepath.Join(mockRoot, "doctrine_container.xml")

	c := NewContainerConfig()
	c.WorkspaceRoot = mockRoot
	c.SetContainerXMLPaths([]string{containerPath})
	c.LoadFromXML(NewAutoloadMap())

	require.NotEmpty(t, c.DoctrineDrivers, "should discover doctrine drivers")

	hasAttribute := false
	hasXML := false
	for _, d := range c.DoctrineDrivers {
		switch d.Kind {
		case DriverKindAttribute:
			hasAttribute = true
			t.Logf("AttributeDriver: namespace=%q paths=%v", d.Namespace, d.Paths)
		case DriverKindXML:
			hasXML = true
			t.Logf("XmlDriver: namespace=%q paths=%v", d.Namespace, d.Paths)
		}
	}

	require.True(t, hasAttribute, "should find AttributeDriver")
	require.True(t, hasXML, "should find XmlDriver")

	// Verify specific drivers
	for _, d := range c.DoctrineDrivers {
		if d.Kind == DriverKindAttribute && d.Namespace == "App\\Entity" {
			require.NotEmpty(t, d.Paths)
			assert.Equal(t, filepath.Join(mockRoot, "Entity"), d.Paths[0])
		}
		if d.Kind == DriverKindXML && d.Namespace == "App\\Entity" {
			require.NotEmpty(t, d.Paths)
			assert.Equal(t, filepath.Join(mockRoot, "vendor", "doctrine"), d.Paths[0])
		}
	}

	t.Logf("Total drivers discovered: %d", len(c.DoctrineDrivers))
}
