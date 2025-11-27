package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinyvision/vimfony/internal/translations"
	"github.com/tliron/commonlog"
)

func (c *ContainerConfig) LoadTranslations() {
	logger := commonlog.GetLoggerf("vimfony.config")

	var resources []string
	seenResources := make(map[string]struct{})

	// Find translations directory relative to container XML
	for _, xmlPath := range c.ContainerXMLPaths {
		absXmlPath := xmlPath
		if !filepath.IsAbs(absXmlPath) {
			absXmlPath = filepath.Join(c.WorkspaceRoot, absXmlPath)
		}

		containerDir := filepath.Dir(absXmlPath)
		translationsDir := filepath.Join(containerDir, "translations")

		// Find *.meta.json files
		entries, err := os.ReadDir(translationsDir)
		if err != nil {
			logger.Debugf("could not read translations dir '%s': %v", translationsDir, err)
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".meta.json") {
				metaPath := filepath.Join(translationsDir, entry.Name())
				metaResources := c.parseMetaJson(metaPath)
				for _, res := range metaResources {
					if _, ok := seenResources[res]; !ok {
						seenResources[res] = struct{}{}
						resources = append(resources, res)
					}
				}
			}
		}
	}

	c.TranslationKeys = translations.Parse(resources)
	logger.Infof("loaded %d translation keys from %d resources", len(c.TranslationKeys), len(resources))
}

func (c *ContainerConfig) parseMetaJson(path string) []string {
	logger := commonlog.GetLoggerf("vimfony.config")
	file, err := os.Open(path)
	if err != nil {
		logger.Warningf("could not open meta.json '%s': %v", path, err)
		return nil
	}
	defer file.Close()

	type resource struct {
		Type     string `json:"@type"`
		Resource string `json:"resource"`
	}
	type meta struct {
		Resources []resource `json:"resources"`
	}

	var m meta
	if err := json.NewDecoder(file).Decode(&m); err != nil {
		logger.Warningf("could not decode meta.json '%s': %v", path, err)
		return nil
	}

	var resources []string
	for _, r := range m.Resources {
		if r.Type == "Symfony\\Component\\Config\\Resource\\FileResource" {
			resPath := r.Resource
			if _, err := os.Stat(resPath); err != nil {
				// Heuristic: if path contains "/vendor/", try to find "vendor/" in workspace and replace prefix.
				if idx := strings.Index(resPath, "/vendor/"); idx != -1 {
					rel := resPath[idx+1:] // vendor/...
					abs := filepath.Join(c.WorkspaceRoot, rel)
					if _, err := os.Stat(abs); err == nil {
						resPath = abs
					}
				} else if idx := strings.Index(resPath, "/src/"); idx != -1 {
					rel := resPath[idx+1:] // src/...
					abs := filepath.Join(c.WorkspaceRoot, rel)
					if _, err := os.Stat(abs); err == nil {
						resPath = abs
					}
				} else if idx := strings.Index(resPath, "/translations/"); idx != -1 {
					// Handle project level translations
					rel := resPath[idx+1:]
					abs := filepath.Join(c.WorkspaceRoot, rel)
					if _, err := os.Stat(abs); err == nil {
						resPath = abs
					}
				}
			}

			resources = append(resources, resPath)
		}
	}
	return resources
}
