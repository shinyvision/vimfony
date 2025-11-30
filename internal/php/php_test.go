package php

import (
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestResolve(t *testing.T) {
	autoloadMap := config.AutoloadMap{
		PSR4: map[string][]string{
			"VendorNamespace\\": {"mock/vendor/"},
		},
	}
	workspaceRoot := "../../"

	store := NewDocumentStore(10)
	store.Configure(autoloadMap, workspaceRoot)

	// Test resolving a class
	path, rng, ok := Resolve(store, "VendorNamespace\\TestClass")
	require.True(t, ok)
	require.Contains(t, path, "mock/vendor/TestClass.php")
	require.Equal(t, uint32(4), rng.Start.Line)

	// Test resolving a non-existent class
	_, _, ok = Resolve(store, "VendorNamespace\\NonExistent")
	require.False(t, ok)
}

func TestFindMethodRange(t *testing.T) {
	autoloadMap := config.AutoloadMap{
		PSR4: map[string][]string{
			"VendorNamespace\\": {"mock/vendor/"},
		},
	}
	workspaceRoot := "../../"

	store := NewDocumentStore(10)
	store.Configure(autoloadMap, workspaceRoot)

	path, _, ok := Resolve(store, "VendorNamespace\\TestClass")
	require.True(t, ok)

	// Test finding an existing method
	rng, found := FindMethodRange(store, path, "index")
	require.True(t, found)
	// index method is at line 7
	require.Equal(t, uint32(6), rng.Start.Line)

	rng, found = FindMethodRange(store, path, "__invoke")
	require.True(t, found)

	// Test finding a non-existent method
	_, ok = FindMethodRange(store, path, "nonExistentMethod")
	require.False(t, ok)
}

func TestPathAt(t *testing.T) {
	content := `<?php
namespace App;

use VendorNamespace\TestClass;

class MyClass {
	public function test() {
		$a = new TestClass();
	}
}
`
	store := NewDocumentStore(10)
	dummyPath := "/tmp/dummy.php"
	doc := NewDocument()
	doc.Update([]byte(content), nil, store)
	store.RegisterOpen(dummyPath, doc)

	path, ok := PathAt(store, dummyPath, protocol.Position{Line: 7, Character: 12}) // Middle of TestClass
	require.True(t, ok)
	require.Equal(t, "TestClass", path)
}
