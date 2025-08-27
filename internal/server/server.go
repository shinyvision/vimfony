package server

import (
	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/state"
	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

const lsName = "vimfony"

var version = "0.0.4"

type Server struct {
	config *config.Config
	state  *state.State
	h      protocol.Handler
}

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
		TextDocumentCompletion: s.onCompletion,
	}
	return s
}

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
	caps.CompletionProvider = &protocol.CompletionOptions{
		TriggerCharacters: []string{"@"},
	}

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
			if phpp, ok := m["php_path"]; ok {
				if str, ok := phpp.(string); ok && str != "" {
					s.config.PhpPath = str
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
	s.state.SetDocument(p.TextDocument.URI, p.TextDocument.Text, p.TextDocument.LanguageID)
	return nil
}

func (s *Server) didChange(_ *glsp.Context, p *protocol.DidChangeTextDocumentParams) error {
	doc, ok := s.state.GetDocument(p.TextDocument.URI)
	if !ok {
		return nil
	}

	text := doc.Text
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
	s.state.SetDocument(p.TextDocument.URI, text, doc.LanguageID)
	return nil
}

func (s *Server) didClose(_ *glsp.Context, p *protocol.DidCloseTextDocumentParams) error {
	s.state.DeleteDocument(p.TextDocument.URI)
	return nil
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
