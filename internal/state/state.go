package state

import (
	"strings"
	"sync"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/shinyvision/vimfony/internal/analyzer"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
)

// State manages the document state for the language server.
type State struct {
	mu       sync.RWMutex
	docs     map[protocol.DocumentUri]*Document
	docStore *php.DocumentStore
}

func NewState(store *php.DocumentStore) *State {
	return &State{
		docs:     make(map[protocol.DocumentUri]*Document),
		docStore: store,
	}
}

// GetDocument retrieves a document from the state.
func (s *State) GetDocument(uri protocol.DocumentUri) (*Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.docs[uri]
	return doc, ok
}

// SetDocument adds or updates a document in the state.
func (s *State) SetDocument(uri protocol.DocumentUri, text string, languageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingDoc, ok := s.docs[uri]; ok {
		// We probably never set the document if it already exists, but this doesn't hurt
		existingDoc.Text = text
		existingDoc.lines = strings.Split(text, "\n")
		return
	}
	doc := NewDocument(languageID, text)
	path := utils.UriToPath(string(uri))
	if doc.Analyzer != nil {
		if dsa, ok := doc.Analyzer.(analyzer.DocumentStoreAware); ok {
			dsa.SetDocumentStore(s.docStore)
		}
		if dpa, ok := doc.Analyzer.(analyzer.DocumentPathAware); ok {
			dpa.SetDocumentPath(path)
		}
	}
	s.docs[uri] = doc
	if doc.Analyzer != nil {
		doc.Analyzer.Changed([]byte(text), nil)
	}
}

func (s *State) ChangeDocument(uri protocol.DocumentUri, text string, change *sitter.InputEdit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingDoc, ok := s.docs[uri]; ok {
		existingDoc.Text = text
		existingDoc.lines = strings.Split(text, "\n")
		if existingDoc.Analyzer != nil {
			existingDoc.Analyzer.Changed([]byte(text), change)
		}
	}
}

// DeleteDocument removes a document from the state.
func (s *State) DeleteDocument(uri protocol.DocumentUri) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingDoc, ok := s.docs[uri]; ok {
		if existingDoc.Analyzer != nil {
			existingDoc.Analyzer.Close()
		}
	}
	delete(s.docs, uri)
}
