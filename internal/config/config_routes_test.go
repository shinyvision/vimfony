package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRoutesMapMergesAllPaths(t *testing.T) {
	tmpDir := t.TempDir()

	firstDir := filepath.Join(tmpDir, "first")
	secondDir := filepath.Join(tmpDir, "second")
	require.NoError(t, os.MkdirAll(firstDir, 0o755))
	require.NoError(t, os.MkdirAll(secondDir, 0o755))

	firstRoutes := filepath.Join(firstDir, "url_generating_routes.php")
	secondRoutes := filepath.Join(secondDir, "url_generating_routes.php")

	firstContent := `<?php
return [
    'first_route' => [['slug'], ['_controller' => 'App\\Controller\\FirstController::index']],
];
`
	secondContent := `<?php
return [
    'second_route' => [['id'], ['_controller' => 'App\\Controller\\SecondController::show']],
];
`

	require.NoError(t, os.WriteFile(firstRoutes, []byte(firstContent), 0o644))
	require.NoError(t, os.WriteFile(secondRoutes, []byte(secondContent), 0o644))

	cfg := NewConfig()
	cfg.PhpPath = "/usr/bin/php"
	cfg.Container.WorkspaceRoot = tmpDir
	cfg.Container.SetContainerXMLPaths([]string{
		filepath.Join("first", "services.xml"),
		filepath.Join("missing", "services.xml"),
		filepath.Join("second", "services.xml"),
	})

	cfg.LoadRoutesMap()

	expected := RoutesMap{
		"first_route": Route{
			Name:       "first_route",
			Parameters: []string{"slug"},
			Controller: "App\\Controller\\FirstController",
			Action:     "index",
		},
		"second_route": Route{
			Name:       "second_route",
			Parameters: []string{"id"},
			Controller: "App\\Controller\\SecondController",
			Action:     "show",
		},
	}

	assert.Equal(t, expected, cfg.Routes)
}
