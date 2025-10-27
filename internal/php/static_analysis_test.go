package php

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestStaticAnalyzerIndexesInheritedMethods(t *testing.T) {
	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)

	psr4 := config.Psr4Map{
		"VendorNamespace\\": []string{"vendor"},
	}

	doc := NewDocument()
	doc.SetWorkspaceRoot(mockRoot)
	doc.SetPsr4Map(psr4)

	testPath := filepath.Join(mockRoot, "vendor", "TestClass.php")
	testURI := utils.PathToURI(testPath)
	doc.SetURI(testURI)

	data, err := os.ReadFile(testPath)
	require.NoError(t, err)
	require.NoError(t, doc.Update(data, nil))

	index := doc.Index()
	require.NotEmpty(t, index.PublicFunctions)
	require.True(t, len(index.Classes) > 0)

	expectedURI := utils.PathToURI(filepath.Join(mockRoot, "vendor", "FooClass.php"))
	expectedURI2 := utils.PathToURI(filepath.Join(mockRoot, "vendor", "BarClass.php"))
	found := false
	found2 := false
	for _, fn := range index.PublicFunctions {
		if fn.Name == "TestClass::bar" {
			require.Equal(t, expectedURI, fn.URI)
			found = true
		}
		if fn.Name == "TestClass::baz" {
			require.Equal(t, expectedURI2, fn.URI)
			found2 = true
		}
	}
	require.True(t, found, "expected inherited method TestClass::bar to be indexed")
	require.True(t, found2, "expected inherited method TestClass::baz to be indexed")
}

func TestClassInfoResolvesUseAliasExtends(t *testing.T) {
	code := []byte(`<?php
namespace Example;

use VendorNamespace\FooClass as AliasClass;
use VendorNamespace\BarClass;
use VendorNamespace\BazClass;
use VendorNamespace\QuxClass;

class Derived extends AliasClass {}
class QuxClass {}
class BazClass extends QuxClass {}
class BarClass extends BazClass {}
class One extends BarClass {}
`)

	doc := NewDocument()
	mockRoot, err := filepath.Abs("../../mock")
	require.NoError(t, err)
	doc.SetWorkspaceRoot(mockRoot)
	doc.SetPsr4Map(config.Psr4Map{
		"VendorNamespace\\": []string{"vendor"},
	})
	require.NoError(t, doc.Update(code, nil))

	var found bool
	for _, info := range doc.Index().Classes {
		if info.Name == "One" {
			require.Equal(t, []string{
				"VendorNamespace\\BarClass",
				"VendorNamespace\\BazClass",
				"VendorNamespace\\QuxClass",
			}, info.Extends)
		}
		if info.Name == "Derived" {
			require.Equal(t, []string{
				"VendorNamespace\\FooClass",
				"VendorNamespace\\BarClass",
				"VendorNamespace\\BazClass",
				"VendorNamespace\\QuxClass",
			}, info.Extends)
			found = true
		}
	}
	require.True(t, found, "expected Derived class metadata to be collected")
}
