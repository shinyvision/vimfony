package config

import (
	"bufio"
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type ContainerConfig struct {
	WorkspaceRoot     string
	ContainerXMLPath  string
	Roots             []string
	BundleRoots       map[string][]string
	ServiceClasses    map[string]string
	ServiceAliases    map[string]string
	TwigFunctions     map[string]protocol.Location
	ServiceReferences map[string]int
}

func NewContainerConfig() *ContainerConfig {
	return &ContainerConfig{
		Roots:             []string{"templates"},
		BundleRoots:       make(map[string][]string),
		ServiceClasses:    make(map[string]string),
		ServiceAliases:    make(map[string]string),
		TwigFunctions:     make(map[string]protocol.Location),
		ServiceReferences: make(map[string]int),
	}
}

// Populates the Config.
func (c *ContainerConfig) LoadFromXML(psr4Map Psr4Map) {
	logger := commonlog.GetLoggerf("vimfony.config")
	if c.ContainerXMLPath == "" {
		return
	}

	absPath := c.ContainerXMLPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(c.WorkspaceRoot, absPath)
	}

	f, err := os.Open(absPath)
	if err != nil {
		logger.Warningf("cannot read container_xml_path: %v", err)
		return
	}
	defer f.Close()

	dec := xml.NewDecoder(f)
	dec.Strict = false

	const targetServiceID = "twig.loader.native_filesystem"

	inTargetService := false
	depth := 0
	inAddPathCall := false
	inArgument := false
	var argBuf strings.Builder
	var args []string

	addedBare := 0
	addedBundle := 0
	bundlesTouched := map[string]struct{}{}
	foundService := false

	c.ServiceClasses = make(map[string]string)
	c.ServiceAliases = make(map[string]string)
	c.ServiceReferences = make(map[string]int)

	var serviceID string
	var serviceClass string
	serviceDepth := 0

	for {
		tok, err := dec.Token()
		if err != nil {
			if err.Error() != "EOF" {
				logger.Warningf("error while parsing XML: %v", err)
			}
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			local := t.Name.Local

			// Parse service definitions and aliases
			if local == "service" {
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
					// A service id does not contain spaces, but sometimes the container xml registers them.
					if !isAbstract && id != "" && !strings.Contains(id, " ") {
						serviceID = id
						if class != "" {
							c.ServiceClasses[id] = class
							serviceClass = class
						} else if alias != "" {
							c.ServiceAliases[id] = alias
							serviceClass = ""
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
					c.indexTwigFunctions(serviceClass, psr4Map)
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

			// Parse twig paths using the common service addPath calls
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
					foundService = true
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

		case xml.EndElement:
			local := t.Name.Local

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
						logger.Infof("XML <call addPath> args: %#v", args)

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
										bundlesTouched[bundle] = struct{}{}
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

	if !foundService {
		logger.Warningf("container_xml_path: service id '%s' not found; no bundle paths loaded from XML", targetServiceID)
	}

	logger.Infof(
		"container_xml_path: loaded %d bare roots and %d bundle paths across %d bundles from XML",
		addedBare, addedBundle, len(bundlesTouched),
	)
}

func (c *ContainerConfig) indexTwigFunctions(class string, psr4Map Psr4Map) {
	logger := commonlog.GetLoggerf("vimfony.config")
	path, ok := Psr4Resolve(class, psr4Map, c.WorkspaceRoot)
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
