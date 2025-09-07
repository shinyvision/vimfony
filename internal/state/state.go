package state

import (
	"strings"
	"sync"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// State manages the document state for the language server.
type State struct {
	mu   sync.RWMutex
	docs map[protocol.DocumentUri]*Document
}

func NewState() *State {
	return &State{
		docs: make(map[protocol.DocumentUri]*Document),
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
		if existingDoc.Analyzer != nil {
			existingDoc.Analyzer.Changed([]byte(text), nil)
		}
		return
	}
	doc := NewDocument(languageID, text)
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
