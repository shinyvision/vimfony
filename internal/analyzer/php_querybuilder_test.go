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
