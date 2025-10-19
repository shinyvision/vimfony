package config

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Route struct {
	Name       string
	Parameters []string
}

type RoutesMap map[string]Route

func GetRoutesMap(routesFile, phpPath string) (RoutesMap, error) {
	// It is important to use the absolute path to the file, otherwise php will not find it.
	absRoutesFile, err := filepath.Abs(routesFile)
	if err != nil {
		return nil, fmt.Errorf("could not get absolute path for %s: %w", routesFile, err)
	}

	cmd := exec.Command(phpPath, "-r", fmt.Sprintf("echo json_encode(require '%s');", absRoutesFile))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("could not execute php script: %w", err)
	}

	// Parse the raw JSON into a map[string][]any
	// The structure is: route_name => [parameters, metadata, ...]
	// We only care about index 0 (parameters array)
	var rawRoutes map[string][]any
	if err := json.Unmarshal(out, &rawRoutes); err != nil {
		return nil, fmt.Errorf("could not unmarshal json: %w", err)
	}

	// Build the RoutesMap from the raw data
	routesMap := make(RoutesMap)
	for name, routeData := range rawRoutes {
		// Exclude routes with backslashes (controller FQNs used as route names)
		if strings.Contains(name, "\\") {
			continue
		}

		if len(routeData) == 0 {
			continue
		}

		// The first element is the parameters array
		paramsInterface, ok := routeData[0].([]any)
		if !ok {
			continue
		}

		// Convert any slice to string slice
		params := make([]string, 0, len(paramsInterface))
		for _, p := range paramsInterface {
			if paramStr, ok := p.(string); ok {
				params = append(params, paramStr)
			}
		}

		routesMap[name] = Route{
			Name:       name,
			Parameters: params,
		}
	}

	return routesMap, nil
}
