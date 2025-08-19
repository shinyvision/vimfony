package config

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
)

type Psr4Map map[string][]string

func GetPsr4Map(autoloadFile string) (Psr4Map, error) {
	// It is important to use the absolute path to the file, otherwise php will not find it.
	absAutoloadFile, err := filepath.Abs(autoloadFile)
	if err != nil {
		return nil, fmt.Errorf("could not get absolute path for %s: %w", autoloadFile, err)
	}

	cmd := exec.Command("php", "-r", fmt.Sprintf("echo json_encode(require '%s');", absAutoloadFile))
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
