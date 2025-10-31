package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetAutoloadMap(t *testing.T) {
	psr4File, err := filepath.Abs("../../mock/autoload_psr4.php")
	assert.NoError(t, err)
	classmapFile, err := filepath.Abs("../../mock/autoload_classmap.php")
	assert.NoError(t, err)

	autoloadMap, err := GetAutoloadMap(psr4File, classmapFile, "/usr/bin/php")
	assert.NoError(t, err)

	mockDir, err := filepath.Abs("../../mock")
	assert.NoError(t, err)

	expected := AutoloadMap{
		PSR4: map[string][]string{
			"VendorNamespace\\": {filepath.Join(mockDir, "vendor")},
			"BaseNamespace\\":   {filepath.Join(mockDir, "base")},
		},
		Classmap: map[string]string{
			"VendorNamespace\\QuxClass": filepath.Join(filepath.Dir(mockDir), "QuxClass.php"),
		},
	}

	assert.Equal(t, expected.PSR4, autoloadMap.PSR4)
	assert.Equal(t, expected.Classmap, autoloadMap.Classmap)
}
