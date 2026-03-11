package doctrine

import (
	"path/filepath"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/php"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMockRegistry(t *testing.T) *Registry {
	t.Helper()

	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	autoload := config.AutoloadMap{
		PSR4: map[string][]string{
			"App\\": {""},
		},
	}

	store := php.NewDocumentStore(10)
	store.Configure(autoload, mockRoot)

	entityDir := filepath.Join(mockRoot, "Entity")
	xmlDir := filepath.Join(mockRoot, "vendor", "doctrine")

	reg := NewRegistry()
	reg.Configure(
		[]config.DoctrineDriverMapping{
			{
				Kind:      config.DriverKindAttribute,
				Namespace: "App\\Entity",
				Paths:     []string{entityDir},
			},
			{
				Kind:      config.DriverKindXML,
				Namespace: "App\\Entity",
				Paths:     []string{xmlDir},
			},
		},
		autoload,
		mockRoot,
		store,
	)
	return reg
}

func TestRegistry_AttributeEntity(t *testing.T) {
	reg := setupMockRegistry(t)

	fields := reg.MappedFields("App\\Entity\\User")
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindId, fm["id"].Kind)
	assert.Equal(t, FieldKindColumn, fm["email"].Kind)
	assert.Equal(t, FieldKindAssociation, fm["address"].Kind)
	assert.Equal(t, FieldKindAssociation, fm["addresses"].Kind)
	assert.Equal(t, FieldKindAssociation, fm["channels"].Kind)
}

func TestRegistry_XMLMappedEntity(t *testing.T) {
	reg := setupMockRegistry(t)

	// AbstractChannel is only XML-mapped (no ORM attributes in the PHP file)
	fields := reg.MappedFields("App\\Entity\\AbstractChannel")
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindId, fm["id"].Kind)
	assert.Equal(t, FieldKindColumn, fm["code"].Kind)
	assert.Equal(t, FieldKindColumn, fm["name"].Kind)
	assert.Equal(t, FieldKindColumn, fm["enabled"].Kind)
}

func TestRegistry_XMLMappedWithAssociations(t *testing.T) {
	reg := setupMockRegistry(t)

	// Subscription.orm.xml has id, columns, and all four association types
	fields := reg.MappedFields("App\\Entity\\Subscription")
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindId, fm["id"].Kind)
	assert.Equal(t, FieldKindColumn, fm["state"].Kind)
	assert.Equal(t, FieldKindColumn, fm["createdAt"].Kind)
	assert.Equal(t, FieldKindAssociation, fm["customer"].Kind, "many-to-one")
	assert.Equal(t, FieldKindAssociation, fm["payments"].Kind, "one-to-many")
	assert.Equal(t, FieldKindAssociation, fm["channels"].Kind, "many-to-many")
	assert.Equal(t, FieldKindAssociation, fm["config"].Kind, "one-to-one")
}

func TestRegistry_EntityWithAssociations(t *testing.T) {
	reg := setupMockRegistry(t)

	fields := reg.MappedFields("App\\Entity\\User")
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindAssociation, fm["address"].Kind, "ManyToOne")
	assert.Equal(t, "App\\Entity\\Address", fm["address"].TargetEntity)

	assert.Equal(t, FieldKindAssociation, fm["addresses"].Kind, "OneToMany")
	assert.Equal(t, "App\\Entity\\Address", fm["addresses"].TargetEntity)

	assert.Equal(t, FieldKindAssociation, fm["channels"].Kind, "ManyToMany")
	assert.Equal(t, "App\\Entity\\Channel", fm["channels"].TargetEntity)
}

func TestRegistry_IsMapped(t *testing.T) {
	reg := setupMockRegistry(t)

	assert.True(t, reg.IsMapped("App\\Entity\\User"))
	assert.True(t, reg.IsMapped("App\\Entity\\AbstractChannel"))
	assert.False(t, reg.IsMapped("SomeRandom\\UnmappedClass"))
}

func TestRegistry_UnmappedClass(t *testing.T) {
	reg := setupMockRegistry(t)

	fields := reg.MappedFields("SomeRandom\\UnmappedClass")
	assert.Nil(t, fields)
}

func TestRegistry_EntityWithTraits(t *testing.T) {
	reg := setupMockRegistry(t)

	// Product uses TimestampableTrait which has createdAt and updatedAt
	fields := reg.MappedFields("App\\Entity\\Product")
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)

	assert.Equal(t, FieldKindId, fm["id"].Kind)
	assert.Equal(t, FieldKindColumn, fm["name"].Kind)
	assert.Equal(t, FieldKindColumn, fm["enabled"].Kind)

	// Fields from TimestampableTrait
	assert.Contains(t, fm, "createdAt", "should have createdAt from trait")
	assert.Contains(t, fm, "updatedAt", "should have updatedAt from trait")
	assert.Equal(t, FieldKindColumn, fm["createdAt"].Kind)
	assert.Equal(t, FieldKindColumn, fm["updatedAt"].Kind)
}

func TestRegistry_Inheritance(t *testing.T) {
	reg := setupMockRegistry(t)

	// Order extends AbstractOrder (both attribute-mapped)
	fields := reg.MappedFields("App\\Entity\\Order")
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)

	assert.Contains(t, fm, "paymentCompletedAt")
	assert.Contains(t, fm, "channel")
	assert.Contains(t, fm, "notImportant")

	// Inherited from AbstractOrder
	assert.Contains(t, fm, "createdAt", "should inherit createdAt from AbstractOrder")
}

func TestRegistry_CrossDriverInheritance(t *testing.T) {
	reg := setupMockRegistry(t)

	// Channel (attribute-mapped) extends AbstractChannel (XML-mapped)
	fields := reg.MappedFields("App\\Entity\\Channel")
	require.NotEmpty(t, fields)

	fm := fieldsByName(fields)

	assert.Contains(t, fm, "customField")
	assert.Equal(t, FieldKindColumn, fm["customField"].Kind)

	// Inherited from XML-mapped AbstractChannel
	assert.Contains(t, fm, "id", "should inherit id from XML parent")
	assert.Contains(t, fm, "code", "should inherit code from XML parent")
	assert.Contains(t, fm, "name", "should inherit name from XML parent")
	assert.Contains(t, fm, "enabled", "should inherit enabled from XML parent")
}

func TestRegistry_AssociationTargetEntity(t *testing.T) {
	reg := setupMockRegistry(t)

	target := reg.AssociationTargetEntity("App\\Entity\\User", "channels")
	assert.Equal(t, "App\\Entity\\Channel", target)

	target = reg.AssociationTargetEntity("App\\Entity\\User", "address")
	assert.Equal(t, "App\\Entity\\Address", target)

	target = reg.AssociationTargetEntity("App\\Entity\\User", "email")
	assert.Empty(t, target)

	target = reg.AssociationTargetEntity("App\\Entity\\User", "nonexistent")
	assert.Empty(t, target)
}
