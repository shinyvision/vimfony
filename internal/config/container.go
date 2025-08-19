package config

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/tliron/commonlog"
)

type ContainerConfig struct {
	WorkspaceRoot    string
	ContainerXMLPath string
	Roots            []string
	BundleRoots      map[string][]string
	ServiceClasses   map[string]string
	ServiceAliases   map[string]string
}

func NewContainerConfig() *ContainerConfig {
	return &ContainerConfig{
		Roots:          []string{"templates"},
		BundleRoots:    make(map[string][]string),
		ServiceClasses: make(map[string]string),
		ServiceAliases: make(map[string]string),
	}
}

// Populates the Config.
func (c *ContainerConfig) LoadFromXML() {
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
				id := ""
				class := ""
				alias := ""
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "id":
						id = a.Value
					case "class":
						class = a.Value
					case "alias":
						alias = a.Value
					}
				}
				if id != "" {
					if class != "" {
						c.ServiceClasses[id] = class
					} else if alias != "" {
						c.ServiceAliases[id] = alias
					}
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

