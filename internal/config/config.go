package config

import (
	"path/filepath"

	"github.com/tliron/commonlog"
)

type Config struct {
	Container *ContainerConfig
	Psr4      Psr4Map
	VendorDir string
}

func NewConfig() *Config {
	return &Config{
		Container: NewContainerConfig(),
		Psr4:      make(Psr4Map),
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

	psr4Map, err := GetPsr4Map(absPath)
	if err != nil {
		logger.Warningf("could not load psr4 map: %v", err)
		return
	}

	c.Psr4 = psr4Map
	logger.Infof("loaded %d psr-4 mappings", len(c.Psr4))
}