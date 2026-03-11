package doctrine

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockXMLPath(t *testing.T, filename string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("../../mock/vendor/doctrine", filename))
	require.NoError(t, err)
	return p
}

func TestParseXMLMappingFile_MappedSuperclass(t *testing.T) {
	path := mockXMLPath(t, "AbstractChannel.orm.xml")

	fields, err := parseXMLMappingFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindId, fm["id"].Kind)
	assert.Equal(t, FieldKindColumn, fm["code"].Kind)
	assert.Equal(t, FieldKindColumn, fm["name"].Kind)
	assert.Equal(t, FieldKindColumn, fm["enabled"].Kind)
}

func TestParseXMLMappingFile_Associations(t *testing.T) {
	path := mockXMLPath(t, "Subscription.orm.xml")

	fields, err := parseXMLMappingFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)

	// ID
	assert.Equal(t, FieldKindId, fm["id"].Kind)

	// Regular fields
	assert.Equal(t, FieldKindColumn, fm["state"].Kind)
	assert.Equal(t, FieldKindColumn, fm["createdAt"].Kind)

	// Associations with target entities
	assert.Equal(t, FieldKindAssociation, fm["customer"].Kind, "many-to-one")
	assert.Equal(t, "App\\Entity\\User", fm["customer"].TargetEntity)

	assert.Equal(t, FieldKindAssociation, fm["payments"].Kind, "one-to-many")
	assert.Equal(t, "App\\Entity\\Order", fm["payments"].TargetEntity)

	assert.Equal(t, FieldKindAssociation, fm["channels"].Kind, "many-to-many")
	assert.Equal(t, "App\\Entity\\Channel", fm["channels"].TargetEntity)

	assert.Equal(t, FieldKindAssociation, fm["config"].Kind, "one-to-one")
	assert.Equal(t, "App\\Entity\\Address", fm["config"].TargetEntity)
}

func TestParseXMLMappingFile_Embeddable(t *testing.T) {
	path := mockXMLPath(t, "File.orm.xml")

	fields, err := parseXMLMappingFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindColumn, fm["name"].Kind)
	assert.Equal(t, FieldKindColumn, fm["originalName"].Kind)
	assert.Equal(t, FieldKindColumn, fm["mimeType"].Kind)
	assert.Equal(t, FieldKindColumn, fm["size"].Kind)
}

func TestFindXMLMappingFile(t *testing.T) {
	baseDir, err := filepath.Abs("../../mock/vendor/doctrine")
	require.NoError(t, err)

	result := findXMLMappingFile(
		"App\\Entity\\AbstractChannel",
		"App\\Entity",
		[]string{baseDir},
	)

	require.NotEmpty(t, result)
	assert.Equal(t, filepath.Join(baseDir, "AbstractChannel.orm.xml"), result)
}

func TestFindXMLMappingFile_NotFound(t *testing.T) {
	result := findXMLMappingFile(
		"Some\\Unknown\\Entity",
		"Some\\Unknown",
		[]string{"/nonexistent/path"},
	)
	assert.Empty(t, result)
}

func TestFindXMLMappingFile_WrongNamespace(t *testing.T) {
	result := findXMLMappingFile(
		"Different\\Namespace\\Entity",
		"Some\\Other\\Namespace",
		[]string{"/some/path"},
	)
	assert.Empty(t, result)
}

func fieldsByName(fields []MappedField) map[string]MappedField {
	m := make(map[string]MappedField, len(fields))
	for _, f := range fields {
		m[f.Name] = f
	}
	return m
}
