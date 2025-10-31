package config

import (
	"path/filepath"

	"github.com/tliron/commonlog"
)

type Config struct {
	Container *ContainerConfig
	Psr4      Psr4Map
	Routes    RoutesMap
	VendorDir string
	PhpPath   string
}

func NewConfig() *Config {
	return &Config{
		Container: NewContainerConfig(),
		Psr4:      make(Psr4Map),
		Routes:    make(RoutesMap),
		PhpPath:   "php",
	}
}

func (c *Config) LoadPsr4Map() {
	logger := commonlog.GetLoggerf("vimfony.config")
	if c.VendorDir == "" {
		return
	}

	autoloadFile := filepath.Join(c.VendorDir, "composer", "autoload_psr4.php")
	absPath := autoloadFile
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(c.Container.WorkspaceRoot, absPath)
	}

	psr4Map, err := GetPsr4Map(absPath, c.PhpPath)
	if err != nil {
		logger.Warningf("could not load psr4 map: %v", err)
		return
	}

	c.Psr4 = psr4Map
	logger.Infof("loaded %d psr-4 mappings", len(c.Psr4))
}

func (c *Config) LoadRoutesMap() {
	logger := commonlog.GetLoggerf("vimfony.config")
	if len(c.Container.ContainerXMLPaths) == 0 {
		return
	}

	containerPath := c.Container.ContainerXMLPaths[0]
	if containerPath == "" {
		return
	}
	if !filepath.IsAbs(containerPath) {
		containerPath = filepath.Join(c.Container.WorkspaceRoot, containerPath)
	}

	routesFile := filepath.Join(filepath.Dir(containerPath), "url_generating_routes.php")
	routesMap, err := GetRoutesMap(routesFile, c.PhpPath)
	if err != nil {
		logger.Warningf("could not load routes map: %v", err)
		return
	}

	c.Routes = routesMap
	logger.Infof("loaded %d routes", len(c.Routes))
}
