package config

import (
	"path/filepath"

	"github.com/tliron/commonlog"
)

type DriverKind string

const (
	DriverKindAttribute DriverKind = "attribute"
	DriverKindXML       DriverKind = "xml"
)

type DoctrineDriverMapping struct {
	Kind      DriverKind
	Namespace string
	Paths     []string
}

const metadataDriverServiceID = "doctrine.orm.default_metadata_driver"

type doctrineCollector struct {
	addDriverCalls    map[string][][2]string
	decorators        map[string][2]string
	serviceArgs       map[string]*driverServiceArgs
	inlineServiceArgs map[string]*driverServiceArgs
}

type driverServiceArgs struct {
	class             string
	paths             []string
	keyedPaths        map[string]string
	locatorKeyedPaths map[string]string
}

func newDoctrineCollector() *doctrineCollector {
	return &doctrineCollector{
		addDriverCalls:    make(map[string][][2]string),
		decorators:        make(map[string][2]string),
		serviceArgs:       make(map[string]*driverServiceArgs),
		inlineServiceArgs: make(map[string]*driverServiceArgs),
	}
}

func (dc *doctrineCollector) resolve(serviceClasses map[string]string, serviceAliases map[string]string, workspaceRoot string) []DoctrineDriverMapping {
	logger := commonlog.GetLoggerf("vimfony.doctrine")

	chainID := dc.findChainService(serviceClasses, serviceAliases)
	if chainID == "" {
		logger.Warningf("could not resolve doctrine metadata driver chain")
		return nil
	}

	calls, ok := dc.addDriverCalls[chainID]
	if !ok || len(calls) == 0 {
		logger.Warningf("no addDriver calls found on chain service '%s'", chainID)
		return nil
	}

	var drivers []DoctrineDriverMapping
	for _, call := range calls {
		driverRef := call[0]
		namespace := call[1]
		if driverRef == "" || namespace == "" {
			continue
		}

		cfg := dc.resolveDriver(driverRef, namespace, serviceClasses, serviceAliases, workspaceRoot)
		if cfg != nil {
			drivers = append(drivers, *cfg)
			logger.Infof("doctrine: %s driver for '%s' with %d paths", cfg.Kind, cfg.Namespace, len(cfg.Paths))
		}
	}

	return drivers
}

func (dc *doctrineCollector) findChainService(_ map[string]string, serviceAliases map[string]string) string {
	var candidates []string
	visited := make(map[string]struct{})
	current := metadataDriverServiceID

	for range 20 {
		if _, seen := visited[current]; seen {
			break
		}
		visited[current] = struct{}{}
		candidates = append(candidates, current)

		if alias, ok := serviceAliases[current]; ok {
			current = alias
			continue
		}

		if dec, ok := dc.decorators[current]; ok && dec[1] != "" {
			candidates = append(candidates, dec[1])
			current = dec[1]
			continue
		}

		found := false
		for _, dec := range dc.decorators {
			if dec[0] == current && dec[1] != "" {
				candidates = append(candidates, dec[1])
				current = dec[1]
				found = true
				break
			}
		}
		if found {
			continue
		}

		break
	}

	for _, id := range candidates {
		if _, ok := dc.addDriverCalls[id]; ok {
			return id
		}
	}

	return ""
}

func (dc *doctrineCollector) resolveDriver(serviceID, namespace string, serviceClasses map[string]string, serviceAliases map[string]string, workspaceRoot string) *DoctrineDriverMapping {
	class := resolveServiceClass(serviceID, serviceClasses, serviceAliases)

	args := dc.serviceArgs[serviceID]
	if args != nil && args.class != "" {
		class = args.class
	}

	switch {
	case isDoctrineAttributeDriver(class):
		return dc.resolveAttributeDriver(serviceID, namespace, workspaceRoot)
	case isDoctrineXMLDriver(class):
		return dc.resolveXMLDriver(serviceID, namespace, workspaceRoot)
	case isDoctrineSimplifiedXMLDriver(class):
		return dc.resolveSimplifiedXMLDriver(serviceID, namespace, workspaceRoot)
	default:
		return nil
	}
}

func (dc *doctrineCollector) resolveAttributeDriver(serviceID, namespace, workspaceRoot string) *DoctrineDriverMapping {
	args := dc.serviceArgs[serviceID]
	if args == nil {
		return nil
	}

	paths := make([]string, 0, len(args.paths))
	for _, p := range args.paths {
		paths = append(paths, toAbsPath(p, workspaceRoot))
	}

	return &DoctrineDriverMapping{
		Kind:      DriverKindAttribute,
		Namespace: namespace,
		Paths:     paths,
	}
}

func (dc *doctrineCollector) resolveXMLDriver(serviceID, namespace, workspaceRoot string) *DoctrineDriverMapping {
	args := dc.inlineServiceArgs[serviceID]
	if args == nil {
		args = dc.serviceArgs[serviceID]
	}
	if args == nil {
		return nil
	}

	var paths []string
	for p := range args.keyedPaths {
		paths = append(paths, toAbsPath(p, workspaceRoot))
	}

	return &DoctrineDriverMapping{
		Kind:      DriverKindXML,
		Namespace: namespace,
		Paths:     paths,
	}
}

func (dc *doctrineCollector) resolveSimplifiedXMLDriver(serviceID, namespace, workspaceRoot string) *DoctrineDriverMapping {
	args := dc.serviceArgs[serviceID]
	if args == nil {
		return nil
	}

	var paths []string
	for p, ns := range args.keyedPaths {
		if ns == namespace {
			paths = append(paths, toAbsPath(p, workspaceRoot))
		}
	}

	if len(paths) == 0 {
		return nil
	}

	return &DoctrineDriverMapping{
		Kind:      DriverKindXML,
		Namespace: namespace,
		Paths:     paths,
	}
}

func resolveServiceClass(serviceID string, classes map[string]string, aliases map[string]string) string {
	visited := make(map[string]struct{})
	current := serviceID
	for range 20 {
		if _, seen := visited[current]; seen {
			break
		}
		visited[current] = struct{}{}
		if class, ok := classes[current]; ok {
			return class
		}
		if alias, ok := aliases[current]; ok {
			current = alias
			continue
		}
		break
	}
	return ""
}

func isDoctrineAttributeDriver(class string) bool {
	return class == "Doctrine\\ORM\\Mapping\\Driver\\AttributeDriver"
}

func isDoctrineXMLDriver(class string) bool {
	return class == "Doctrine\\ORM\\Mapping\\Driver\\XmlDriver"
}

func isDoctrineSimplifiedXMLDriver(class string) bool {
	return class == "Doctrine\\ORM\\Mapping\\Driver\\SimplifiedXmlDriver"
}

func toAbsPath(path, workspaceRoot string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspaceRoot, path)
}

func isDoctrineDriverClass(class string) bool {
	return isDoctrineAttributeDriver(class) ||
		isDoctrineXMLDriver(class) ||
		isDoctrineSimplifiedXMLDriver(class)
}

func isSymfonyFileLocatorClass(class string) bool {
	return class == "Doctrine\\Persistence\\Mapping\\Driver\\SymfonyFileLocator"
}

