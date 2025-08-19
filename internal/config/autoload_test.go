package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPsr4Map(t *testing.T) {
	mockAutoloadFile, err := filepath.Abs("../../mock/autoload_psr4.php")
	assert.NoError(t, err)

	psr4Map, err := GetPsr4Map(mockAutoloadFile)
	assert.NoError(t, err)

	mockDir, err := filepath.Abs("../../mock")
	assert.NoError(t, err)

	expected := Psr4Map{
		"VendorNamespace\\": []string{filepath.Join(mockDir, "vendor")},
		"BaseNamespace\\":   []string{filepath.Join(mockDir, "base")},
	}

	assert.Equal(t, expected, psr4Map)
}
