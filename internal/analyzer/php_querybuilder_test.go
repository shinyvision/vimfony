package analyzer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/doctrine"
	"github.com/shinyvision/vimfony/internal/php"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func prepareQueryBuilderTest(t *testing.T, uri, fileContent string) *phpAnalyzer {
	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	an := NewPHPAnalyzer().(*phpAnalyzer)

	container := &config.ContainerConfig{
		WorkspaceRoot: mockRoot,
	}
	an.SetContainerConfig(container)

	autoload := config.AutoloadMap{
		PSR4: map[string][]string{
			"App\\": {""},
		},
	}

	store := php.NewDocumentStore(10)
	store.Configure(autoload, mockRoot)
	an.SetDocumentStore(store)
	an.SetAutoloadMap(&autoload)
	an.SetDocumentPath(uri)

	// Set up doctrine registry with attribute + XML drivers for mock entities
	entityDir := filepath.Join(mockRoot, "Entity")
	xmlMappingDir := filepath.Join(mockRoot, "vendor", "doctrine")
	reg := doctrine.NewRegistry()
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
				Paths:     []string{xmlMappingDir},
			},
		},
		autoload,
		mockRoot,
		store,
		map[string]string{
			"App\\Entity\\AddressInterface": "App\\Entity\\Address",
		},
	)
	an.SetDoctrineRegistry(reg)

	err = an.Changed([]byte(fileContent), nil)
	require.NoError(t, err)

	return an
}

func TestQueryBuilderAliasResolutionWithRepositoryAndCreateQueryBuilder(t *testing.T) {
	content, err := os.ReadFile("../../mock/Repository/UserRepository.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", string(content))

	target := "$qb->where('u."
	pos := positionAfter(t, content, target, len(target))

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "id")
	require.Contains(t, labels, "email")
	require.Contains(t, labels, "address")
}

func TestQueryBuilderAliasResolutionWithJoin(t *testing.T) {
	content, err := os.ReadFile("../../mock/Repository/UserRepository.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", string(content))

	target := "$qb->andWhere('a."
	pos := positionAfter(t, content, target, len(target))

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	// Mapped fields of the joined entity should appear
	require.Contains(t, labels, "street")
	require.Contains(t, labels, "city")

	// Non-mapped properties must NOT appear
	require.NotContains(t, labels, "internalNote")
}

func TestQueryBuilderJoinOnCollectionAssociation(t *testing.T) {
	content, err := os.ReadFile("../../mock/Repository/UserRepository.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", string(content))

	target := "$qb->andWhere('addr."
	pos := positionAfter(t, content, target, len(target))

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items, "should resolve Collection-typed association via doctrine targetEntity")

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	// Should show Address entity's mapped fields, not Collection methods
	require.Contains(t, labels, "street")
	require.Contains(t, labels, "city")
	require.NotContains(t, labels, "internalNote")
}

func TestQueryBuilderJoinOnChannelAssociation(t *testing.T) {
	content, err := os.ReadFile("../../mock/Repository/UserRepository.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", string(content))

	target := "$qb->andWhere('c."
	pos := positionAfter(t, content, target, len(target))

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items, "should resolve ManyToMany Channel association across attribute→XML inheritance")

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	// Fields from XML-mapped AbstractChannel superclass
	require.Contains(t, labels, "id")
	require.Contains(t, labels, "code")
	require.Contains(t, labels, "name")
	require.Contains(t, labels, "enabled")

	// Field from attribute-mapped Channel child
	require.Contains(t, labels, "customField")
}

func TestQueryBuilderAliasResolutionWithFromClass(t *testing.T) {
	content, err := os.ReadFile("../../mock/Repository/UserRepository.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", string(content))

	target := "$qb->andWhere('aa."
	pos := positionAfter(t, content, target, len(target)) // using exact target end

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "street")
	require.Contains(t, labels, "city")
}

func TestQueryBuilderAliasResolutionWithGetRepositoryChain(t *testing.T) {
	content, err := os.ReadFile("../../mock/Service/OrderService.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/OrderService.php", string(content))

	target := "->andWhere('o."
	pos := positionAfter(t, content, target, len(target)) // using exact target end

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	require.Contains(t, labels, "paymentCompletedAt")
	require.Contains(t, labels, "channel")
	require.Contains(t, labels, "createdAt")
}

func TestQueryBuilderChainedJoinCompletion(t *testing.T) {
	content, err := os.ReadFile("../../mock/Service/DivisionService.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/DivisionService.php", string(content))

	target := "->andWhere('c."
	pos := positionAfter(t, content, target, len(target))

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items, "should resolve join alias in chained method calls")

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	// Fields from XML-mapped AbstractChannel superclass
	require.Contains(t, labels, "id")
	require.Contains(t, labels, "code")
	require.Contains(t, labels, "name")
	require.Contains(t, labels, "enabled")

	// Field from attribute-mapped Channel child
	require.Contains(t, labels, "customField")
}

func TestQueryBuilderPropertyDocumentation(t *testing.T) {
	content, err := os.ReadFile("../../mock/Service/OrderService.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/OrderService.php", string(content))

	target := "->andWhere('o."
	pos := positionAfter(t, content, target, len(target))

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	// Build maps for documentation and detail
	docMap := make(map[string]string)
	detailMap := make(map[string]string)
	for _, item := range items {
		if item.Documentation != nil {
			if mc, ok := item.Documentation.(protocol.MarkupContent); ok {
				docMap[item.Label] = mc.Value
			}
		}
		if item.Detail != nil {
			detailMap[item.Label] = *item.Detail
		}
	}

	// Detail should show FQN::$property (kind)
	require.Contains(t, detailMap["paymentCompletedAt"], "App\\Entity\\Order::$paymentCompletedAt")
	require.Contains(t, detailMap["channel"], "App\\Entity\\Order::$channel")
	require.Contains(t, detailMap["createdAt"], "::$createdAt")

	// paymentCompletedAt has an ORM attribute above it
	paymentDoc := docMap["paymentCompletedAt"]
	require.NotContains(t, paymentDoc, "---")
	require.Contains(t, paymentDoc, "<?php")
	require.Contains(t, paymentDoc, "#[ORM\\Column")
	require.Contains(t, paymentDoc, "private \\DateTime $paymentCompletedAt")

	// channel has an attribute above it → should contain the attribute
	channelDoc := docMap["channel"]
	require.Contains(t, channelDoc, "#[ORM\\Column")
	require.Contains(t, channelDoc, "private string $channel")

	// notImportant has another property before it → should only show the definition
	notImportantDoc := docMap["notImportant"]
	require.Contains(t, notImportantDoc, "private int $notImportant")
	require.NotContains(t, notImportantDoc, "$channel") // should not leak previous property
}

func TestQueryBuilderNestedChainedJoinCompletion(t *testing.T) {
	inlineContent := `<?php
namespace App\Service;

use App\Entity\User;

class SomeService
{
    private $entityManager;

    public function findNested()
    {
        $result = $this->entityManager->getRepository(User::class)
            ->createQueryBuilder('u')
            ->join('u.channels', 'c')
            ->join('c.currencies', 'cur')
            ->andWhere('cur.')
            ->getQuery()
            ->getOneOrNullResult();
    }
}
`
	an := prepareQueryBuilderTest(t, "/tmp/SomeService.php", inlineContent)

	pos := positionAfter(t, []byte(inlineContent), "'cur.", 5)

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items, "should resolve nested join where intermediate association is inherited from XML-mapped parent")

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	// currencies targets Address, so we should get Address fields
	require.Contains(t, labels, "street")
	require.Contains(t, labels, "city")
}

func TestQueryBuilderJoinOnlyShowsAssociations(t *testing.T) {
	inlineContent := `<?php
namespace App\Repository;

class UserRepository
{
    public function find()
    {
        $qb = $this->createQueryBuilder('u');
        $qb->join('u.', 'a');
    }
}
`
	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", inlineContent)

	pos := positionAfter(t, []byte(inlineContent), "'u.", 3)

	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	var labels []string
	for _, item := range items {
		labels = append(labels, item.Label)
	}

	// Only associations should appear in join methods
	require.Contains(t, labels, "address")
	require.Contains(t, labels, "addresses")
	require.Contains(t, labels, "channels")

	// Non-association fields must NOT appear
	require.NotContains(t, labels, "id")
	require.NotContains(t, labels, "email")
}

func TestQueryBuilderDefinitionOnOwnField(t *testing.T) {
	content, err := os.ReadFile("../../mock/Repository/UserRepository.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", string(content))

	// Cursor on "id" in "$qb->where('u.id = 1')"
	pos := positionAfter(t, content, "u.id", 3)

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)

	uri := string(locs[0].URI)
	require.Contains(t, uri, "Entity/User.php")

	// $id is on line 12
	require.Equal(t, uint32(11), locs[0].Range.Start.Line)
	require.Equal(t, uint32(16), locs[0].Range.Start.Character) // column of '$'
	require.Equal(t, uint32(19), locs[0].Range.End.Character)   // end of '$id'
}

func TestQueryBuilderDefinitionOnJoinedField(t *testing.T) {
	content, err := os.ReadFile("../../mock/Repository/UserRepository.php")
	require.NoError(t, err)

	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", string(content))

	// Cursor on "street" in "$qb->andWhere('a.street = :street')"
	pos := positionAfter(t, content, "a.street =", 4)

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs)

	uri := string(locs[0].URI)
	require.Contains(t, uri, "Entity/Address.php")

	// $street is on line 9
	require.Equal(t, uint32(8), locs[0].Range.Start.Line)
	require.Equal(t, uint32(19), locs[0].Range.Start.Character) // column of '$'
	require.Equal(t, uint32(26), locs[0].Range.End.Character)   // end of '$street'
}

func TestQueryBuilderDefinitionOnInheritedField(t *testing.T) {
	inlineContent := `<?php
namespace App\Repository;

use App\Entity\User;

class UserRepository
{
    public function findByChannel()
    {
        $qb = $this->createQueryBuilder('u');
        $qb->join('u.channels', 'c');
        $qb->andWhere('c.code = :code');
    }
}
`
	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", inlineContent)

	pos := positionAfter(t, []byte(inlineContent), "c.code", 4)

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.NotEmpty(t, locs, "should resolve inherited field from XML-mapped AbstractChannel")

	uri := string(locs[0].URI)
	require.Contains(t, uri, "Entity/AbstractChannel.php")
}

func TestQueryBuilderDefinitionOnUnmappedField(t *testing.T) {
	inlineContent := `<?php
namespace App\Repository;

class UserRepository
{
    public function find()
    {
        $qb = $this->createQueryBuilder('u');
        $qb->where('u.nonexistent = 1');
    }
}
`
	an := prepareQueryBuilderTest(t, "/tmp/UserRepository.php", inlineContent)

	pos := positionAfter(t, []byte(inlineContent), "u.nonexistent", 5)

	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.Empty(t, locs, "should return nothing for unmapped field")
}
