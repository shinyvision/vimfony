package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/tliron/commonlog"
)

type Config struct {
	Container *ContainerConfig
	Autoload  AutoloadMap
	Routes    RoutesMap
	VendorDir string
	PhpPath   string
}

func NewConfig() *Config {
	return &Config{
		Container: NewContainerConfig(),
		Autoload:  NewAutoloadMap(),
		Routes:    make(RoutesMap),
		PhpPath:   "php",
	}
}

func (c *Config) LoadAutoloadMap() {
	logger := commonlog.GetLoggerf("vimfony.config")
	if c.VendorDir == "" {
		return
	}

	psr4File := filepath.Join(c.VendorDir, "composer", "autoload_psr4.php")
	classmapFile := filepath.Join(c.VendorDir, "composer", "autoload_classmap.php")

	if !filepath.IsAbs(psr4File) {
		psr4File = filepath.Join(c.Container.WorkspaceRoot, psr4File)
	}
	if !filepath.IsAbs(classmapFile) {
		classmapFile = filepath.Join(c.Container.WorkspaceRoot, classmapFile)
	}

	autoloadMap, err := GetAutoloadMap(psr4File, classmapFile, c.PhpPath)
	if err != nil {
		logger.Warningf("could not load autoload map: %v", err)
		return
	}

	c.Autoload = autoloadMap
	logger.Infof(
		"loaded %d psr-4 mappings and %d classmap entries",
		len(c.Autoload.PSR4),
		len(c.Autoload.Classmap),
	)
}

func (c *Config) LoadRoutesMap() {
	logger := commonlog.GetLoggerf("vimfony.config")
	if len(c.Container.ContainerXMLPaths) == 0 {
		return
	}

	c.Routes = make(RoutesMap)

	loaded := 0
	for idx, containerPath := range c.Container.ContainerXMLPaths {
		if containerPath == "" {
			continue
		}
		if !filepath.IsAbs(containerPath) {
			containerPath = filepath.Join(c.Container.WorkspaceRoot, containerPath)
		}

		routesFile := filepath.Join(filepath.Dir(containerPath), "url_generating_routes.php")

		if _, err := os.Stat(routesFile); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			logger.Warningf("cannot access routes file for container_xml_path[%d] '%s': %v", idx, routesFile, err)
			continue
		}

		routesMap, err := GetRoutesMap(routesFile, c.PhpPath)
		if err != nil {
			logger.Warningf("could not load routes map from '%s': %v", routesFile, err)
			continue
		}

		for name, route := range routesMap {
			c.Routes[name] = route
		}

		loaded++
	}

	if loaded > 0 {
		logger.Infof("loaded %d routes from %d route files", len(c.Routes), loaded)
	}
}
