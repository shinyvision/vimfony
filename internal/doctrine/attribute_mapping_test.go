package doctrine

import (
	"context"
	"testing"

	phpforest "github.com/alexaandru/go-sitter-forest/php"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseTestPHP(t *testing.T, src string) sitter.Node {
	t.Helper()
	parser := sitter.NewParser()
	lang := sitter.NewLanguage(phpforest.GetLanguage())
	parser.SetLanguage(lang)
	tree, err := parser.ParseString(context.Background(), nil, []byte(src))
	require.NoError(t, err)
	t.Cleanup(func() { tree.Close() })
	return tree.RootNode()
}

func TestExtractAttributeFields_SimpleEntity(t *testing.T) {
	src := `<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class PaymentTerm
{
    #[ORM\Id]
    #[ORM\GeneratedValue]
    #[ORM\Column]
    private ?int $id = null;

    #[ORM\Column(length: 255)]
    private ?string $code = null;

    #[ORM\Column(type: 'integer')]
    private int $days = 0;
}
`
	root := parseTestPHP(t, src)
	fields := extractAttributeFields(root, []byte(src), "ORM")

	require.Len(t, fields, 3)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindId, fm["id"].Kind)
	assert.Equal(t, FieldKindColumn, fm["code"].Kind)
	assert.Equal(t, FieldKindColumn, fm["days"].Kind)
}

func TestExtractAttributeFields_Associations(t *testing.T) {
	src := `<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;
use Doctrine\Common\Collections\Collection;

#[ORM\Entity]
class Ticket
{
    #[ORM\Id]
    #[ORM\GeneratedValue]
    #[ORM\Column(type: 'integer')]
    private ?int $id = null;

    #[ORM\ManyToOne(AdminUser::class)]
    private ?AdminUser $assignee = null;

    #[ORM\OneToMany(mappedBy: 'ticket', targetEntity: TicketReply::class)]
    private Collection $replies;

    #[ORM\ManyToMany(targetEntity: Tag::class)]
    private Collection $tags;

    #[ORM\OneToOne(inversedBy: 'profile', targetEntity: Profile::class)]
    private ?Profile $profile = null;

    #[ORM\Column(type: 'text')]
    private string $message = '';
}
`
	root := parseTestPHP(t, src)
	fields := extractAttributeFields(root, []byte(src), "ORM")

	require.Len(t, fields, 6)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindId, fm["id"].Kind)
	assert.Equal(t, FieldKindAssociation, fm["assignee"].Kind)
	assert.Equal(t, FieldKindAssociation, fm["replies"].Kind)
	assert.Equal(t, FieldKindAssociation, fm["tags"].Kind)
	assert.Equal(t, FieldKindAssociation, fm["profile"].Kind)
	assert.Equal(t, FieldKindColumn, fm["message"].Kind)
}

func TestExtractAttributeFields_Embedded(t *testing.T) {
	src := `<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class Invoice
{
    #[ORM\Id]
    #[ORM\Column]
    private ?int $id = null;

    #[ORM\Embedded(class: Address::class)]
    private Address $address;
}
`
	root := parseTestPHP(t, src)
	fields := extractAttributeFields(root, []byte(src), "ORM")

	require.Len(t, fields, 2)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindId, fm["id"].Kind)
	assert.Equal(t, FieldKindEmbedded, fm["address"].Kind)
}

func TestExtractAttributeFields_GroupedAttributes(t *testing.T) {
	// Test the grouped attribute syntax: #[ORM\Id, ORM\Column(type: 'integer')]
	src := `<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class Foo
{
    #[
        ORM\Id,
        ORM\GeneratedValue,
        ORM\Column(type: 'integer'),
    ]
    private ?int $id = null;
}
`
	root := parseTestPHP(t, src)
	fields := extractAttributeFields(root, []byte(src), "ORM")

	require.Len(t, fields, 1)
	assert.Equal(t, "id", fields[0].Name)
	assert.Equal(t, FieldKindId, fields[0].Kind)
}

func TestExtractAttributeFields_NoORM(t *testing.T) {
	src := `<?php
namespace App\Entity;

class PlainClass
{
    private ?int $id = null;
    private string $name = '';
}
`
	root := parseTestPHP(t, src)
	fields := extractAttributeFields(root, []byte(src), "ORM")

	assert.Empty(t, fields)
}

func TestResolveORMAlias(t *testing.T) {
	src := `<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

class Foo {}
`
	root := parseTestPHP(t, src)
	alias := resolveORMAlias(root, []byte(src))
	assert.Equal(t, "ORM", alias)
}

func TestResolveORMAlias_Custom(t *testing.T) {
	src := `<?php
use Doctrine\ORM\Mapping as Mapping;

class Foo {}
`
	root := parseTestPHP(t, src)
	alias := resolveORMAlias(root, []byte(src))
	assert.Equal(t, "Mapping", alias)
}

func TestResolveORMAlias_NotPresent(t *testing.T) {
	src := `<?php
use App\Entity\SomeClass;

class Foo {}
`
	root := parseTestPHP(t, src)
	alias := resolveORMAlias(root, []byte(src))
	assert.Empty(t, alias)
}

func TestExtractTraitUses(t *testing.T) {
	src := `<?php
namespace App\Entity;

use App\Traits\TimestampableTrait;
use App\Traits\SoftDeletableTrait;

class Product
{
    use TimestampableTrait;
    use SoftDeletableTrait;

    private ?int $id = null;
}
`
	root := parseTestPHP(t, src)
	classNode := findClassNode(root, []byte(src))
	require.False(t, classNode.IsNull())

	uses := map[string]string{
		"timestampabletrait": "App\\Traits\\TimestampableTrait",
		"softdeletabletrait": "App\\Traits\\SoftDeletableTrait",
	}

	traits := extractTraitUses(classNode, []byte(src), uses, "App\\Entity")

	require.Len(t, traits, 2)
	assert.Contains(t, traits, "App\\Traits\\TimestampableTrait")
	assert.Contains(t, traits, "App\\Traits\\SoftDeletableTrait")
}

func TestExtractTraitUses_UnqualifiedWithNamespace(t *testing.T) {
	// When a trait is in the same namespace and not imported via use
	src := `<?php
namespace App\Entity;

class Product
{
    use ProductTrait;
}
`
	root := parseTestPHP(t, src)
	classNode := findClassNode(root, []byte(src))
	require.False(t, classNode.IsNull())

	traits := extractTraitUses(classNode, []byte(src), map[string]string{}, "App\\Entity")

	require.Len(t, traits, 1)
	assert.Equal(t, "App\\Entity\\ProductTrait", traits[0])
}

func TestExtractAttributeFields_TraitWithAttributes(t *testing.T) {
	src := `<?php
namespace App\Traits;

use Doctrine\ORM\Mapping as ORM;

trait TimestampableTrait
{
    #[ORM\Column(type: 'datetime')]
    private ?\DateTime $createdAt = null;

    #[ORM\Column(type: 'datetime', nullable: true)]
    private ?\DateTime $updatedAt = null;
}
`
	root := parseTestPHP(t, src)
	fields := extractAttributeFields(root, []byte(src), "ORM")

	require.Len(t, fields, 2)

	fm := fieldsByName(fields)
	assert.Equal(t, FieldKindColumn, fm["createdAt"].Kind)
	assert.Equal(t, FieldKindColumn, fm["updatedAt"].Kind)
}
