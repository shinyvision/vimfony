package analyzer

import (
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/stretchr/testify/require"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestOnCodeAction_GenerateGettersSetters(t *testing.T) {
	content := []byte(`<?php

class User {
    private string $name;
    private ?int $age;
    private bool $isActive;
    private $unknown;
    
    public function getName(): string {
        return $this->name;
    }
}
`)

	analyzer := NewPHPAnalyzer()
	store := php.NewDocumentStore(10)
	store.Configure(config.AutoloadMap{}, "")

	pa := analyzer.(*phpAnalyzer)
	pa.SetDocumentStore(store)
	pa.SetDocumentPath("/test.php")
	require.NoError(t, analyzer.Changed(content, nil))

	pos := protocol.Position{Line: 3, Character: 4}
	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.php"},
		Range:        protocol.Range{Start: pos, End: pos},
	}

	actions, err := pa.OnCodeAction(&glsp.Context{}, params)
	require.NoError(t, err)

	require.Len(t, actions, 3)

	findAction := func(title string) *protocol.CodeAction {
		for _, a := range actions {
			if a.Title == title {
				return &a
			}
		}
		return nil
	}

	gsAction := findAction("Generate getters & setters")
	require.NotNil(t, gsAction)
	gsEdit := gsAction.Edit.Changes[protocol.DocumentUri("file:///test.php")][0]
	gsText := gsEdit.NewText

	require.Contains(t, gsText, "getAge(): ?int")
	require.Contains(t, gsText, "setAge(?int $age)")
	require.Contains(t, gsText, "isActive(): bool")
	require.Contains(t, gsText, "setIsActive(bool $isActive)")
	require.Contains(t, gsText, "getUnknown(): mixed")
	require.Contains(t, gsText, "setUnknown(mixed $unknown)")

	require.NotContains(t, gsText, "getName")
	require.NotContains(t, gsText, "setName")

	gAction := findAction("Generate getters")
	require.NotNil(t, gAction)
	gText := gAction.Edit.Changes[protocol.DocumentUri("file:///test.php")][0].NewText
	require.Contains(t, gText, "getAge")
	require.Contains(t, gText, "isActive")
	require.Contains(t, gText, "getUnknown")
	require.NotContains(t, gText, "getName")
	require.NotContains(t, gText, "setAge")

	sAction := findAction("Generate setters")
	require.NotNil(t, sAction)
	sText := sAction.Edit.Changes[protocol.DocumentUri("file:///test.php")][0].NewText
	require.Contains(t, sText, "setName")
	require.Contains(t, sText, "setAge")
	require.Contains(t, sText, "setIsActive")
	require.Contains(t, sText, "setUnknown")
	require.NotContains(t, sText, "getName")

	require.Equal(t, uint32(4), gsEdit.Range.Start.Line)
}

func TestOnCodeAction_BoolNaming(t *testing.T) {
	content := []byte(`<?php
class BoolTest {
    private bool $isValid;
    private ?bool $enabled;
    private bool $hasPermission;
}
`)
	analyzer := NewPHPAnalyzer()
	store := php.NewDocumentStore(10)
	store.Configure(config.AutoloadMap{}, "")

	path := "/bool.php"
	pa := analyzer.(*phpAnalyzer)
	pa.SetDocumentStore(store)
	pa.SetDocumentPath(path)
	require.NoError(t, analyzer.Changed(content, nil))

	pos := protocol.Position{Line: 2, Character: 4}
	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(utils.PathToURI(path))},
		Range:        protocol.Range{Start: pos, End: pos},
	}

	actions, err := pa.OnCodeAction(&glsp.Context{}, params)
	require.NoError(t, err)

	gAction := actions[1]
	require.Equal(t, "Generate getters", gAction.Title)
	text := gAction.Edit.Changes[protocol.DocumentUri(utils.PathToURI(path))][0].NewText

	require.Contains(t, text, "function isValid()")
	require.Contains(t, text, "function isEnabled()")
	require.Contains(t, text, "function isHasPermission()")
}

func TestOnCodeAction_InsertionInWhitespace(t *testing.T) {
	content := []byte(`<?php
class InsertionTest {
    private $a;

    private $b;
}
`)
	analyzer := NewPHPAnalyzer()
	store := php.NewDocumentStore(10)
	store.Configure(config.AutoloadMap{}, "")

	path := "/insertion.php"
	pa := analyzer.(*phpAnalyzer)
	pa.SetDocumentStore(store)
	pa.SetDocumentPath(path)
	require.NoError(t, analyzer.Changed(content, nil))

	pos := protocol.Position{Line: 3, Character: 0}
	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(utils.PathToURI(path))},
		Range:        protocol.Range{Start: pos, End: pos},
	}

	actions, err := pa.OnCodeAction(&glsp.Context{}, params)
	require.NoError(t, err)

	require.NotEmpty(t, actions)
	action := actions[0]
	edit := action.Edit.Changes[protocol.DocumentUri(utils.PathToURI(path))][0]

	require.Equal(t, uint32(4), edit.Range.Start.Line)
}

func TestOnCodeAction_AlternateGetter(t *testing.T) {
	content := []byte(`<?php
class AlternateTest {
    private bool $active;
    
    public function getActive(): bool {
        return $this->active;
    }
}
`)
	analyzer := NewPHPAnalyzer()
	store := php.NewDocumentStore(10)
	store.Configure(config.AutoloadMap{}, "")

	path := "/alternate.php"
	pa := analyzer.(*phpAnalyzer)
	pa.SetDocumentStore(store)
	pa.SetDocumentPath(path)
	require.NoError(t, analyzer.Changed(content, nil))

	pos := protocol.Position{Line: 2, Character: 4}
	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(utils.PathToURI(path))},
		Range:        protocol.Range{Start: pos, End: pos},
	}

	actions, err := pa.OnCodeAction(&glsp.Context{}, params)
	require.NoError(t, err)

	require.Len(t, actions, 1)
	require.Equal(t, "Generate setters", actions[0].Title)
	require.Contains(t, actions[0].Edit.Changes[protocol.DocumentUri(utils.PathToURI(path))][0].NewText, "setActive")
}

func TestOnCodeAction_NamespacedResolution(t *testing.T) {

	content := []byte(`<?php
namespace App\Entity;

use App\Model\User;
use App\Service\OldService as Legacy;

class TestEntity {
    private int $id;
    private User $user;
    private Legacy $alias;
    private Status $same;
    private \Other\Lib\Clazz $other;
}
`)

	analyzerVar := NewPHPAnalyzer()
	store := php.NewDocumentStore(10)
	store.Configure(config.AutoloadMap{}, "/project")

	path := "/project/src/Entity/TestEntity.php"
	pa := analyzerVar.(*phpAnalyzer)
	pa.SetDocumentStore(store)
	pa.SetDocumentPath(path)
	require.NoError(t, analyzerVar.Changed(content, nil))

	pos := protocol.Position{Line: 7, Character: 4}
	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(utils.PathToURI(path))},
		Range:        protocol.Range{Start: pos, End: pos},
	}

	actions, err := pa.OnCodeAction(&glsp.Context{}, params)
	require.NoError(t, err)

	require.NotEmpty(t, actions)

	var gettersAction *protocol.CodeAction
	for _, action := range actions {
		if action.Title == "Generate getters" {
			gettersAction = &action
			break
		}
	}
	require.NotNil(t, gettersAction)

	newText := gettersAction.Edit.Changes[protocol.DocumentUri(utils.PathToURI(path))][0].NewText

	require.Contains(t, newText, ": int")

	require.Contains(t, newText, "function getUser(): User")

	require.Contains(t, newText, "function getAlias(): Legacy")

	require.Contains(t, newText, "function getSame(): \\Status")

	require.Contains(t, newText, "function getOther(): \\Other\\Lib\\Clazz")
}
