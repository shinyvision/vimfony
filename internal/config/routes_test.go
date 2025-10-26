package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetRoutesMap(t *testing.T) {
	mockRoutesFile, err := filepath.Abs("../../mock/url_generating_routes.php")
	assert.NoError(t, err)

	routesMap, err := GetRoutesMap(mockRoutesFile, "/usr/bin/php")
	assert.NoError(t, err)

	expected := RoutesMap{
		"_wdt": Route{
			Name:       "_wdt",
			Parameters: []string{"token"},
			Controller: "web_profiler.controller.profiler",
			Action:     "toolbarAction",
		},
		"app_foo_bar": Route{
			Name:       "app_foo_bar",
			Parameters: []string{"id"},
			Controller: "App\\Foo\\BarController",
			Action:     "index",
		},
	}

	assert.Equal(t, expected, routesMap)
}
