package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type AutoloadMap struct {
	PSR4     map[string][]string
	Classmap map[string]string
}

func NewAutoloadMap() AutoloadMap {
	return AutoloadMap{
		PSR4:     make(map[string][]string),
		Classmap: make(map[string]string),
	}
}

func (m AutoloadMap) IsEmpty() bool {
	return len(m.PSR4) == 0 && len(m.Classmap) == 0
}

func GetAutoloadMap(psr4File, classmapFile, phpPath string) (AutoloadMap, error) {
	result := NewAutoloadMap()

	if psr4File != "" {
		if err := loadAutoloadSection(psr4File, phpPath, &result.PSR4); err != nil {
			return AutoloadMap{}, fmt.Errorf("could not load psr4 map: %w", err)
		}
	}

	if classmapFile != "" {
		if err := loadAutoloadSection(classmapFile, phpPath, &result.Classmap); err != nil {
			return AutoloadMap{}, fmt.Errorf("could not load classmap: %w", err)
		}
	}

	return result, nil
}

func loadAutoloadSection(autoloadFile, phpPath string, target any) error {
	data, err := executeAutoloadPHP(autoloadFile, phpPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("could not unmarshal json: %w", err)
	}
	return nil
}

func executeAutoloadPHP(autoloadFile, phpPath string) ([]byte, error) {
	absAutoloadFile, err := filepath.Abs(autoloadFile)
	if err != nil {
		return nil, fmt.Errorf("could not get absolute path for %s: %w", autoloadFile, err)
	}

	// The PHP code strips out unsafe keys.
	cmd := exec.Command(phpPath, "-r", fmt.Sprintf("$scm=[];$cm=require'%s';foreach($cm as $k=>$v){$j=json_encode([$k=>$v]);if(is_string($j))$scm[$k]=$v;}echo json_encode($scm);", absAutoloadFile))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("could not execute php script: %w", err)
	}
	return out, nil
}

func AutoloadResolve(className string, autoloadMap AutoloadMap, workspaceRoot string) (string, bool) {
	if path, ok := autoloadMap.Classmap[className]; ok {
		if resolved, ok := resolveClassmapPath(path, workspaceRoot); ok {
			return resolved, true
		}
	}

	for namespace, paths := range autoloadMap.PSR4 {
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

func resolveClassmapPath(path, workspaceRoot string) (string, bool) {
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workspaceRoot, path)
	}
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, true
	}
	return "", false
}
