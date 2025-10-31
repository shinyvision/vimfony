package php

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/utils"
)

type storedDocument struct {
	path   string
	doc    *Document
	isOpen bool
}

// DocumentStore maintains a bounded set of parsed PHP documents.
type DocumentStore struct {
	mu       sync.Mutex
	max      int
	entries  []*storedDocument
	index    map[string]*storedDocument
	autoload config.AutoloadMap
	root     string
}

func (s *DocumentStore) contextSnapshot() (config.AutoloadMap, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoload, s.root
}

// NewDocumentStore constructs a store with the provided maximum size.
func NewDocumentStore(max int) *DocumentStore {
	if max <= 0 {
		max = 1000
	}
	return &DocumentStore{
		max:     max,
		entries: make([]*storedDocument, 0, max),
		index:   make(map[string]*storedDocument),
	}
}

// Configure updates the shared context injected into any stored document.
func (s *DocumentStore) Configure(autoload config.AutoloadMap, workspaceRoot string) {
	s.mu.Lock()
	s.autoload = autoload
	s.root = workspaceRoot
	entries := append([]*storedDocument(nil), s.entries...)
	s.mu.Unlock()

	for _, entry := range entries {
		if entry != nil && entry.doc != nil {
			configureDocumentContext(entry.doc, entry.path, autoload, workspaceRoot)
		}
	}
}

// RegisterOpen registers a document as currently open. The document will not be
// evicted until Close is invoked for the same path.
func (s *DocumentStore) RegisterOpen(path string, doc *Document) {
	if doc == nil {
		return
	}
	path = normalizePath(path)
	if path == "" {
		return
	}

	autoload, root := s.contextSnapshot()
	configureDocumentContext(doc, path, autoload, root)

	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.index[path]; ok {
		entry.doc = doc
		entry.isOpen = true
		s.moveToEndLocked(entry)
		return
	}

	entry := &storedDocument{
		path:   path,
		doc:    doc,
		isOpen: true,
	}
	s.entries = append(s.entries, entry)
	s.index[path] = entry
	s.ensureCapacityLocked()
}

// Close marks a document as no longer open. It becomes eligible for eviction.
func (s *DocumentStore) Close(path string) {
	path = normalizePath(path)
	if path == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.index[path]; ok {
		entry.isOpen = false
	}
}

// Retrieves or loads (and caches) a document for the given path.
func (s *DocumentStore) Get(path string) (*Document, error) {
	path = normalizePath(path)
	if path == "" {
		return nil, errors.New("empty path")
	}

	s.mu.Lock()
	autoload := s.autoload
	root := s.root
	if entry, ok := s.index[path]; ok && entry.doc != nil {
		doc := entry.doc
		s.moveToEndLocked(entry)
		s.mu.Unlock()
		configureDocumentContext(doc, path, autoload, root)
		return doc, nil
	}
	s.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	doc := NewDocument()
	configureDocumentContext(doc, path, autoload, root)
	if err := doc.Update(data, nil); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.index[path]; ok {
		if entry.doc == nil {
			entry.doc = doc
		}
		s.moveToEndLocked(entry)
		return entry.doc, nil
	}

	entry := &storedDocument{
		path: path,
		doc:  doc,
	}
	s.entries = append(s.entries, entry)
	s.index[path] = entry
	s.ensureCapacityLocked()
	return doc, nil
}

func (s *DocumentStore) moveToEndLocked(entry *storedDocument) {
	if len(s.entries) == 0 {
		return
	}
	idx := -1
	for i, e := range s.entries {
		if e == entry {
			idx = i
			break
		}
	}
	if idx < 0 || idx == len(s.entries)-1 {
		return
	}
	s.entries = append(s.entries[:idx], s.entries[idx+1:]...)
	s.entries = append(s.entries, entry)
}

func (s *DocumentStore) ensureCapacityLocked() {
	for len(s.entries) > s.max {
		evicted := false
		for i, entry := range s.entries {
			if entry.isOpen {
				continue
			}
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			delete(s.index, entry.path)
			if entry.doc != nil {
				entry.doc.Close()
			}
			evicted = true
			break
		}
		if !evicted {
			break
		}
	}
}

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func configureDocumentContext(doc *Document, path string, autoload config.AutoloadMap, workspaceRoot string) {
	if doc == nil {
		return
	}
	if path != "" {
		doc.SetURI(utils.PathToURI(path))
	}
	if workspaceRoot != "" {
		doc.SetWorkspaceRoot(workspaceRoot)
	}
	doc.SetAutoloadMap(autoload)
}
