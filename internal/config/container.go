package config

import (
	"bufio"
	"encoding/xml"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/shinyvision/vimfony/internal/translations"
	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type ContainerConfig struct {
	WorkspaceRoot     string
	ContainerXMLPaths []string
	Roots             []string
	BundleRoots       map[string][]string
	ServiceClasses    map[string]string
	ServiceAliases    map[string]string
	TwigFunctions     map[string]protocol.Location
	ServiceReferences map[string]int
	TranslationRoots  []string
	TranslationKeys   translations.TranslationMap
	DefaultLocale     string
	twigTemplates     []string
	twigTemplateSig   string
	twigMu            sync.Mutex
}

const targetServiceID = "twig.loader.native_filesystem"

type containerLoadStats struct {
	addedBare      int
	addedBundle    int
	bundlesTouched map[string]struct{}
	foundService   bool
}

func NewContainerConfig() *ContainerConfig {
	return &ContainerConfig{
		Roots:             []string{"templates"},
		TranslationRoots:  []string{"translations"},
		BundleRoots:       make(map[string][]string),
		ServiceClasses:    make(map[string]string),
		ServiceAliases:    make(map[string]string),
		TwigFunctions:     make(map[string]protocol.Location),
		ServiceReferences: make(map[string]int),
		TranslationKeys:   make(translations.TranslationMap),
		DefaultLocale:     "en", // Default to 'en' if not found
	}
}

// SetContainerXMLPaths replaces the configured container XML paths while keeping order and uniqueness.
func (c *ContainerConfig) SetContainerXMLPaths(paths []string) {
	seen := make(map[string]struct{}, len(paths))
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		filtered = append(filtered, p)
	}

	c.ContainerXMLPaths = filtered
}

// Populates the Config.
func (c *ContainerConfig) LoadFromXML(autoloadMap AutoloadMap) {
	logger := commonlog.GetLoggerf("vimfony.config")
	if len(c.ContainerXMLPaths) == 0 {
		return
	}

	c.ServiceClasses = make(map[string]string)
	c.ServiceAliases = make(map[string]string)
	c.ServiceReferences = make(map[string]int)
	c.TwigFunctions = make(map[string]protocol.Location)
	c.twigMu.Lock()
	c.twigTemplates = nil
	c.twigTemplateSig = ""
	c.twigMu.Unlock()

	totalBare := 0
	totalBundle := 0
	totalBundles := make(map[string]struct{})
	processed := 0

	for idx, relPath := range c.ContainerXMLPaths {
		if relPath == "" {
			continue
		}

		absPath := relPath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(c.WorkspaceRoot, absPath)
		}

		stats, err := c.loadContainerXML(absPath, autoloadMap)
		if err != nil {
			logger.Warningf("cannot read container_xml_path[%d] '%s': %v", idx, relPath, err)
			continue
		}

		processed++
		totalBare += stats.addedBare
		totalBundle += stats.addedBundle
		for bundle := range stats.bundlesTouched {
			totalBundles[bundle] = struct{}{}
		}
		if !stats.foundService {
			logger.Warningf("container_xml_path[%d] '%s': service id '%s' not found; no bundle paths loaded from XML", idx, absPath, targetServiceID)
		}
	}

	if processed == 0 {
		return
	}

	logger.Infof(
		"container_xml_path: loaded %d bare roots and %d bundle paths across %d bundles from %d XML files",
		totalBare, totalBundle, len(totalBundles), processed,
	)
}

func (c *ContainerConfig) loadContainerXML(absPath string, autoloadMap AutoloadMap) (containerLoadStats, error) {
	logger := commonlog.GetLoggerf("vimfony.config")
	stats := containerLoadStats{
		bundlesTouched: make(map[string]struct{}),
		foundService:   false,
	}

	f, err := os.Open(absPath)
	if err != nil {
		return stats, err
	}
	defer f.Close()

	dec := xml.NewDecoder(f)
	dec.Strict = false

	inTargetService := false
	depth := 0
	inAddPathCall := false
	inArgument := false
	var argBuf strings.Builder
	var args []string

	addedBare := 0
	addedBundle := 0

	serviceDepth := 0
	var serviceID string
	var serviceClass string

	inParameter := false
	parameterKey := ""
	var paramBuf strings.Builder

	for {
		tok, err := dec.Token()
		if err != nil {
			if err.Error() != "EOF" {
				logger.Warningf("error while parsing XML '%s': %v", absPath, err)
			}
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			local := t.Name.Local

			if local == "parameter" {
				for _, a := range t.Attr {
					if a.Name.Local == "key" {
						parameterKey = a.Value
						break
					}
				}
				if parameterKey == "kernel.default_locale" {
					inParameter = true
					paramBuf.Reset()
				}
			} else if local == "service" {
				if serviceDepth == 0 {
					id := ""
					class := ""
					alias := ""
					isAbstract := false
					for _, a := range t.Attr {
						switch a.Name.Local {
						case "id":
							id = a.Value
						case "class":
							class = a.Value
						case "alias":
							alias = a.Value
						case "abstract":
							isAbstract = a.Value == "true"
						}
					}

					serviceID = ""
					serviceClass = ""
					if !isAbstract && id != "" && !strings.Contains(id, " ") {
						serviceID = id
						if class != "" {
							if _, exists := c.ServiceClasses[id]; !exists {
								c.ServiceClasses[id] = class
								serviceClass = class
							}
						} else if alias != "" {
							if _, classExists := c.ServiceClasses[id]; !classExists {
								if _, aliasExists := c.ServiceAliases[id]; !aliasExists {
									c.ServiceAliases[id] = alias
								}
							}
						}
					}
				}
				serviceDepth++
			} else if serviceDepth > 0 && local == "tag" {
				name := ""
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						name = a.Value
						break
					}
				}
				if name == "twig.extension" && serviceID != "" && serviceClass != "" {
					c.indexTwigFunctions(serviceClass, autoloadMap)
				}
			} else if serviceDepth > 0 && local == "argument" {
				isServiceArg := false
				serviceIDRef := ""
				for _, a := range t.Attr {
					if a.Name.Local == "type" && a.Value == "service" {
						isServiceArg = true
					}
					if a.Name.Local == "id" {
						serviceIDRef = a.Value
					}
				}
				if isServiceArg && serviceIDRef != "" {
					c.ServiceReferences[serviceIDRef]++
				}
			}

			if !inTargetService && local == "service" {
				id := ""
				for _, a := range t.Attr {
					if a.Name.Local == "id" {
						id = a.Value
						break
					}
				}
				if id == targetServiceID {
					inTargetService = true
					stats.foundService = true
					depth = 1
					continue
				}
			} else if inTargetService {
				depth++

				if local == "call" {
					method := ""
					for _, a := range t.Attr {
						if a.Name.Local == "method" {
							method = a.Value
							break
						}
					}
					if method == "addPath" {
						inAddPathCall = true
						args = args[:0]
					}
				} else if inAddPathCall && local == "argument" {
					inArgument = true
					argBuf.Reset()
				}
			}

		case xml.CharData:
			if inTargetService && inAddPathCall && inArgument {
				argBuf.Write(t)
			}
			if inParameter {
				paramBuf.Write(t)
			}

		case xml.EndElement:
			local := t.Name.Local

			if local == "parameter" {
				if inParameter {
					c.DefaultLocale = strings.TrimSpace(paramBuf.String())
					logger.Infof("Found kernel.default_locale: %s", c.DefaultLocale)
					inParameter = false
				}
			}

			if local == "service" {
				serviceDepth--
				if serviceDepth == 0 {
					serviceID = ""
					serviceClass = ""
				}
			}

			if inTargetService {
				if inAddPathCall && local == "argument" {
					inArgument = false
					val := strings.TrimSpace(argBuf.String())
					args = append(args, val)
					argBuf.Reset()
				} else if local == "call" && inAddPathCall {
					inAddPathCall = false
					if len(args) != 0 {
						logger.Infof("container_xml_path '%s': XML <call addPath> args: %#v", absPath, args)

						base := strings.TrimSpace(args[0])
						if base != "" {
							if !filepath.IsAbs(base) {
								base = filepath.Join(c.WorkspaceRoot, base)
							}
							if len(args) >= 2 {
								bundle := strings.TrimSpace(args[1])
								if strings.HasPrefix(bundle, "!") {
									// Do nothing
								} else {
									before := len(c.BundleRoots[bundle])
									c.BundleRoots[bundle] = utils.AppendUnique(c.BundleRoots[bundle], base)
									if len(c.BundleRoots[bundle]) > before {
										addedBundle++
										stats.bundlesTouched[bundle] = struct{}{}
									}
								}
							} else {
								before := len(c.Roots)
								c.Roots = utils.AppendUnique(c.Roots, base)
								if len(c.Roots) > before {
									addedBare++
								}
							}
						}
					}
				}

				depth--
				if depth == 0 && local == "service" {
					inTargetService = false
				}
			}
		}
	}

	stats.addedBare = addedBare
	stats.addedBundle = addedBundle
	return stats, nil
}

func (c *ContainerConfig) indexTwigFunctions(class string, autoloadMap AutoloadMap) {
	logger := commonlog.GetLoggerf("vimfony.config")
	path, ok := AutoloadResolve(class, autoloadMap, c.WorkspaceRoot)
	if !ok {
		return
	}

	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	type state int
	const (
		SearchingForGetFunctions state = iota
		InGetFunctions
	)

	currentState := SearchingForGetFunctions
	braceLevel := 0
	lineNumber := 0
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		switch currentState {
		case SearchingForGetFunctions:
			if strings.Contains(line, "public function getFunctions()") {
				currentState = InGetFunctions
				braceLevel += strings.Count(line, "{")
				braceLevel -= strings.Count(line, "}")
			}
		case InGetFunctions:
			braceLevel += strings.Count(line, "{")
			braceLevel -= strings.Count(line, "}")
			if braceLevel <= 0 {
				return
			}
			re := regexp.MustCompile(`new\s+TwigFunction\s*\(\s*['"]([^'"]+)['"]`)
			matches := re.FindAllStringSubmatchIndex(line, -1)
			for _, match := range matches {
				if len(match) >= 4 {
					functionName := line[match[2]:match[3]]
					startCol := utf8.RuneCountInString(line[:match[2]])
					endCol := startCol + utf8.RuneCountInString(functionName)
					locRange := protocol.Range{
						Start: protocol.Position{Line: uint32(lineNumber), Character: uint32(startCol)},
						End:   protocol.Position{Line: uint32(lineNumber), Character: uint32(endCol)},
					}
					c.TwigFunctions[functionName] = protocol.Location{URI: "file://" + path, Range: locRange}
					logger.Debugf("indexed twig function '%s' at %s:%d", functionName, path, lineNumber+1)
				}
			}
		}
		lineNumber++
	}
}

// Resolves a service ID to its class name.
func (c *ContainerConfig) ResolveServiceId(serviceID string) (string, bool) {
	// First, check if it's a direct class
	if class, ok := c.ServiceClasses[serviceID]; ok {
		return class, true
	}

	// If not, check if it's an alias and resolve recursively
	resolvedID := serviceID
	for range 10 { // Limit recursion to prevent infinite loops
		if targetID, ok := c.ServiceAliases[resolvedID]; ok {
			resolvedID = targetID
			if class, ok := c.ServiceClasses[resolvedID]; ok {
				return class, true
			}
		} else {
			return "", false
		}
	}
	return "", false
}

// TwigTemplates returns the set of twig template identifiers discovered from configured roots.
func (c *ContainerConfig) TwigTemplates() []string {
	c.twigMu.Lock()
	defer c.twigMu.Unlock()

	sig := c.twigTemplateSignature()
	if sig == c.twigTemplateSig && c.twigTemplates != nil {
		return append([]string(nil), c.twigTemplates...)
	}

	templates := c.collectTwigTemplates()
	c.twigTemplates = templates
	c.twigTemplateSig = sig
	return append([]string(nil), templates...)
}

func (c *ContainerConfig) twigTemplateSignature() string {
	roots := append([]string(nil), c.Roots...)
	sort.Strings(roots)

	bundleNames := make([]string, 0, len(c.BundleRoots))
	for name := range c.BundleRoots {
		bundleNames = append(bundleNames, name)
	}
	sort.Strings(bundleNames)

	parts := make([]string, 0, 2+len(bundleNames))
	parts = append(parts, "workspace:"+c.WorkspaceRoot)
	parts = append(parts, "roots:"+strings.Join(roots, "|"))

	for _, name := range bundleNames {
		bases := append([]string(nil), c.BundleRoots[name]...)
		sort.Strings(bases)
		parts = append(parts, "bundle:"+name+"="+strings.Join(bases, "|"))
	}

	return strings.Join(parts, ";")
}

func (c *ContainerConfig) collectTwigTemplates() []string {
	seen := make(map[string]struct{})
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		value = strings.ReplaceAll(value, "\\", "/")
		if strings.HasPrefix(value, "./") {
			value = value[2:]
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
	}

	for _, root := range c.Roots {
		base := root
		if !filepath.IsAbs(base) {
			base = filepath.Join(c.WorkspaceRoot, base)
		}
		walkTwigFiles(base, func(path string) {
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return
			}
			add(filepath.ToSlash(rel))
		})
	}

	for bundle, bases := range c.BundleRoots {
		if bundle == "" {
			continue
		}
		for _, base := range bases {
			abs := base
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(c.WorkspaceRoot, abs)
			}
			walkTwigFiles(abs, func(path string) {
				rel, err := filepath.Rel(abs, path)
				if err != nil {
					return
				}
				add("@" + bundle + "/" + filepath.ToSlash(rel))
			})
		}
	}

	templates := make([]string, 0, len(seen))
	for value := range seen {
		templates = append(templates, value)
	}
	sort.Strings(templates)
	return templates
}

func walkTwigFiles(base string, fn func(path string)) {
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return
	}
	filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".twig") {
			fn(path)
		}
		return nil
	})
}
