package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Psr4Map map[string][]string

func GetPsr4Map(autoloadFile, phpPath string) (Psr4Map, error) {
	// It is important to use the absolute path to the file, otherwise php will not find it.
	absAutoloadFile, err := filepath.Abs(autoloadFile)
	if err != nil {
		return nil, fmt.Errorf("could not get absolute path for %s: %w", autoloadFile, err)
	}

	cmd := exec.Command(phpPath, "-r", fmt.Sprintf("echo json_encode(require '%s');", absAutoloadFile))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("could not execute php script: %w", err)
	}

	var psr4Map Psr4Map
	if err := json.Unmarshal(out, &psr4Map); err != nil {
		return nil, fmt.Errorf("could not unmarshal json: %w", err)
	}

	return psr4Map, nil
}

func Psr4Resolve(className string, psr4Map Psr4Map, workspaceRoot string) (string, bool) {
	for namespace, paths := range psr4Map {
		if strings.HasPrefix(className, namespace) {
			for _, path := range paths {
				relPath := strings.Replace(className, namespace, "", 1)
				relPath = strings.ReplaceAll(relPath, "\\", string(filepath.Separator)) + ".php"

				absPath := path
				if !filepath.IsAbs(absPath) {
					absPath = filepath.Join(workspaceRoot, path)
				}

				cand := filepath.Join(absPath, relPath)
				if info, err := os.Stat(cand); err == nil && !info.IsDir() {
					return cand, true
				}
			}
		}
	}

	return "", false
}
