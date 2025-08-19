package server

import (
	"regexp"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/state"
	"github.com/shinyvision/vimfony/internal/twig"
	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

const lsName = "vimfony"

var version = "0.1.0"

// Server is the language server.
type Server struct {
	config *config.Config
	state  *state.State
	h      protocol.Handler
}

// NewServer creates a new server.
func NewServer() *Server {
	s := &Server{
		config: config.NewConfig(),
		state:  state.NewState(),
	}
	s.h = protocol.Handler{
		Initialize:             s.initialize,
		Initialized:            s.initialized,
		Shutdown:               s.shutdown,
		SetTrace:               s.setTrace,
		TextDocumentDidOpen:    s.didOpen,
		TextDocumentDidChange:  s.didChange,
		TextDocumentDidClose:   s.didClose,
		TextDocumentDefinition: s.onDefinition,
	}
	return s
}

// Run runs the language server.
func (s *Server) Run() {
	server := glspserver.NewServer(&s.h, lsName, false)
	server.RunStdio()
}

func (s *Server) initialize(_ *glsp.Context, params *protocol.InitializeParams) (any, error) {
	caps := s.h.CreateServerCapabilities()
	openClose := true
	change := protocol.TextDocumentSyncKindIncremental
	caps.TextDocumentSync = &protocol.TextDocumentSyncOptions{
		OpenClose: &openClose,
		Change:    &change,
	}
	defProvider := true
	caps.DefinitionProvider = defProvider

	if params.RootURI != nil {
		s.config.Container.WorkspaceRoot = utils.UriToPath(*params.RootURI)
	} else if len(params.WorkspaceFolders) > 0 {
		s.config.Container.WorkspaceRoot = utils.UriToPath(params.WorkspaceFolders[0].URI)
	} else {
		s.config.Container.WorkspaceRoot = "."
	}

	if params.InitializationOptions != nil {
		if m, ok := params.InitializationOptions.(map[string]any); ok {
			if r, ok := m["roots"]; ok {
				if arr, ok := r.([]any); ok {
					var roots []string
					for _, v := range arr {
						if str, ok := v.(string); ok && str != "" {
							roots = append(roots, str)
						}
					}
					if len(roots) > 0 {
						s.config.Container.Roots = roots
					}
				}
			}
			if cxp, ok := m["container_xml_path"]; ok {
				if str, ok := cxp.(string); ok && str != "" {
					s.config.Container.ContainerXMLPath = str
				}
			}
			if vdp, ok := m["vendor_dir"]; ok {
				if str, ok := vdp.(string); ok && str != "" {
					s.config.VendorDir = str
				}
			}
		}
	}

	s.config.Container.LoadFromXML()
	s.config.LoadPsr4Map()

	logPathStats(s.config, "initialize")

	return protocol.InitializeResult{
		Capabilities: caps,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    lsName,
			Version: &version,
		},
	}, nil
}

func (s *Server) initialized(_ *glsp.Context, _ *protocol.InitializedParams) error { return nil }
func (s *Server) shutdown(_ *glsp.Context) error                                   { return nil }
func (s *Server) setTrace(_ *glsp.Context, p *protocol.SetTraceParams) error {
	protocol.SetTraceValue(p.Value)
	return nil
}

func (s *Server) didOpen(_ *glsp.Context, p *protocol.DidOpenTextDocumentParams) error {
	s.state.SetDocument(p.TextDocument.URI, p.TextDocument.Text)
	return nil
}

func (s *Server) didChange(_ *glsp.Context, p *protocol.DidChangeTextDocumentParams) error {
	text, ok := s.state.GetDocument(p.TextDocument.URI)
	if !ok {
		return nil
	}

	for _, c := range p.ContentChanges {
		switch ch := c.(type) {
		case protocol.TextDocumentContentChangeEventWhole:
			text = ch.Text
		case protocol.TextDocumentContentChangeEvent:
			start := ch.Range.Start.IndexIn(text)
			end := ch.Range.End.IndexIn(text)
			if start >= 0 && end >= start && end <= len(text) {
				text = text[:start] + ch.Text + text[end:]
			}
		}
	}
	s.state.SetDocument(p.TextDocument.URI, text)
	return nil
}

func (s *Server) didClose(_ *glsp.Context, p *protocol.DidCloseTextDocumentParams) error {
	s.state.DeleteDocument(p.TextDocument.URI)
	return nil
}

func (s *Server) onDefinition(_ *glsp.Context, p *protocol.DefinitionParams) (any, error) {
	text, ok := s.state.GetDocument(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if twigPath, ok := twig.PathAt(text, p.Position); ok {
		if target, ok := twig.Resolve(twigPath, s.config.Container); ok {
			loc := protocol.Location{
				URI:   protocol.DocumentUri(utils.PathToURI(target)),
				Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 0}},
			}
			return []protocol.Location{loc}, nil
		}
	}

	if phpClassName, ok := php.PathAt(text, p.Position); ok {
		if target, classRange, ok := php.Resolve(phpClassName, s.config.Psr4, s.config.Container.WorkspaceRoot); ok {
			loc := protocol.Location{
				URI:   protocol.DocumentUri(utils.PathToURI(target)),
				Range: classRange,
			}
			return []protocol.Location{loc}, nil
		}
	}

	serviceIDRe := regexp.MustCompile(`@([a-zA-Z0-9_\.]+)`)
	offset := p.Position.IndexIn(text)

	idxs := serviceIDRe.FindAllStringSubmatchIndex(text, -1)
	for _, m := range idxs {
		if len(m) >= 4 && m[0] <= offset && offset <= m[1] {
			serviceID := text[m[2]:m[3]]
			if className, ok := s.config.Container.ResolveServiceId(serviceID); ok {
				if target, classRange, ok := php.Resolve(className, s.config.Psr4, s.config.Container.WorkspaceRoot); ok {
					loc := protocol.Location{
						URI:   protocol.DocumentUri(utils.PathToURI(target)),
						Range: classRange,
					}
					return []protocol.Location{loc}, nil
				}
			}
		}
	}

	return nil, nil
}

func logPathStats(cfg *config.Config, context string) {
	logger := commonlog.GetLoggerf("vimfony.server")
	totalBundlePaths := 0
	for _, paths := range cfg.Container.BundleRoots {
		totalBundlePaths += len(paths)
	}
	logger.Infof("path stats (%s): %d bare roots, %d bundle paths across %d bundles",
		context, len(cfg.Container.Roots), totalBundlePaths, len(cfg.Container.BundleRoots))
}
