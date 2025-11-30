package php

import (
	"context"
	"sort"
	"sync"
	"time"

	phpforest "github.com/alexaandru/go-sitter-forest/php"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Document maintains a parsed PHP syntax tree together with its static analysis index.
// It owns the tree-sitter parser and decides when static analysis should be re-run.
type Document struct {
	parser          *sitter.Parser
	mu              sync.RWMutex
	tree            *sitter.Tree
	content         []byte
	docURI          string
	workspaceRoot   string
	autoload        config.AutoloadMap
	analyzer        *StaticAnalyzer
	index           IndexedTree
	dirtyRanges     []ByteRange
	analysisTimer   *time.Timer
	analysisVersion int64
	lastAnalyzed    int64
}

// NewDocument constructs a Document ready to track a PHP source file.
func NewDocument() *Document {
	parser := sitter.NewParser()
	lang := sitter.NewLanguage(phpforest.GetLanguage())
	_ = parser.SetLanguage(lang)
	return &Document{
		parser:   parser,
		analyzer: NewStaticAnalyzer(),
		index: IndexedTree{
			Properties: make(map[string][]TypeOccurrence),
			Variables:  make(map[string]FunctionScope),
			Types:      make(map[string][]TypeReference),
			Classes:    make(map[uint32]ClassInfo),
		},
	}
}

// SetURI configures the document URI for downstream analysis.
func (d *Document) SetURI(uri string) {
	d.setContext(uri, d.workspaceRoot, d.autoload)
}

// SetWorkspaceRoot configures the workspace root used for path resolution.
func (d *Document) SetWorkspaceRoot(root string) {
	d.setContext(d.docURI, root, d.autoload)
}

// SetAutoloadMap assigns the Composer autoload map used during static analysis.
func (d *Document) SetAutoloadMap(autoload config.AutoloadMap) {
	d.setContext(d.docURI, d.workspaceRoot, autoload)
}

func (d *Document) setContext(uri, root string, autoload config.AutoloadMap) {
	d.mu.Lock()
	d.docURI = uri
	d.workspaceRoot = root
	d.autoload = autoload
	analyzer := d.analyzer
	d.mu.Unlock()

	if analyzer != nil {
		analyzer.Configure(uri, autoload, root)
	}
}

// Update refreshes the document's content and AST.
func (d *Document) Update(content []byte, change *sitter.InputEdit, store *DocumentStore) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.tree == nil || change == nil {
		// Full re-parse
		if d.tree != nil {
			d.tree.Close()
		}
		parser := sitter.NewParser()
		lang := sitter.NewLanguage(phpforest.GetLanguage())
		_ = parser.SetLanguage(lang)
		tree, err := parser.ParseString(context.Background(), nil, content)
		if err != nil {
			return err
		}
		d.tree = tree
		d.content = content
		d.index = d.analyzer.Update(&d.content, d.tree, nil, store)
		return nil
	}

	// Incremental re-parse
	d.tree.Edit(*change)
	parser := sitter.NewParser()
	lang := sitter.NewLanguage(phpforest.GetLanguage())
	_ = parser.SetLanguage(lang)
	newTree, err := parser.ParseString(context.Background(), d.tree, content)
	if err != nil {
		return err
	}
	d.tree.Close()
	d.tree = newTree
	d.content = content

	dirty := []ByteRange{{Start: uint32(change.StartIndex), End: uint32(change.NewEndIndex)}}
	d.index = d.analyzer.Update(&d.content, d.tree, dirty, store)
	return nil
}

// Close releases resources owned by the document.
func (d *Document) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.analysisTimer != nil {
		d.analysisTimer.Stop()
		d.analysisTimer = nil
	}
	if d.tree != nil {
		d.tree.Close()
		d.tree = nil
	}
	d.content = nil
}

// Read executes the provided function while holding a read lock on the document.
// The callback must not store the tree, content, or index beyond its scope.
func (d *Document) Read(fn func(tree *sitter.Tree, content []byte, index IndexedTree)) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	fn(d.tree, d.content, d.index)
}

// Index returns the most recently computed static analysis index.
func (d *Document) Index() IndexedTree {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.index
}

// GetNodeAt returns the syntax node that spans the provided LSP position together with
// the current file content and static analysis index. The returned content is a copy,
// ensuring callers cannot mutate the underlying buffer.
func (d *Document) GetNodeAt(pos protocol.Position) (sitter.Node, []byte, IndexedTree, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.tree == nil {
		return sitter.Node{}, nil, IndexedTree{}, false
	}

	point, ok := positionToPoint(pos, d.content)
	if !ok {
		return sitter.Node{}, nil, IndexedTree{}, false
	}

	root := d.tree.RootNode()
	if root.IsNull() {
		return sitter.Node{}, nil, IndexedTree{}, false
	}

	node := root.NamedDescendantForPointRange(point, point)
	if node.IsNull() {
		return sitter.Node{}, nil, IndexedTree{}, false
	}

	contentCopy := append([]byte(nil), d.content...)
	return node, contentCopy, d.index, true
}

func (d *Document) recordDirtyRangeLocked(edit *sitter.InputEdit) {
	if edit == nil {
		d.dirtyRanges = nil
		return
	}
	rangeStart := uint32(edit.StartIndex)
	rangeEnd := uint32(edit.NewEndIndex)
	if edit.OldEndIndex > edit.NewEndIndex {
		rangeEnd = uint32(edit.OldEndIndex)
	}
	if rangeStart > rangeEnd {
		rangeStart, rangeEnd = rangeEnd, rangeStart
	}
	if rangeStart == rangeEnd {
		rangeEnd++
	}
	d.dirtyRanges = appendByteRange(d.dirtyRanges, ByteRange{Start: rangeStart, End: rangeEnd})
}

func appendByteRange(ranges []ByteRange, rng ByteRange) []ByteRange {
	ranges = append(ranges, rng)
	if len(ranges) == 0 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start == ranges[j].Start {
			return ranges[i].End < ranges[j].End
		}
		return ranges[i].Start < ranges[j].Start
	})

	merged := make([]ByteRange, 0, len(ranges))
	current := ranges[0]
	for _, r := range ranges[1:] {
		if r.Start <= current.End {
			if r.End > current.End {
				current.End = r.End
			}
			continue
		}
		merged = append(merged, current)
		current = r
	}
	merged = append(merged, current)
	return merged
}

func positionToPoint(pos protocol.Position, content []byte) (sitter.Point, bool) {
	line := int(pos.Line)
	column := int(pos.Character)
	if line < 0 || column < 0 {
		return sitter.Point{}, false
	}

	currentLine := 0
	offset := 0
	for offset < len(content) && currentLine < line {
		if content[offset] == '\n' {
			currentLine++
		}
		offset++
	}

	if currentLine != line {
		return sitter.Point{}, false
	}

	byteColumn := 0
	for offset < len(content) && content[offset] != '\n' && byteColumn < column {
		offset++
		byteColumn++
	}

	if byteColumn < column {
		return sitter.Point{}, false
	}

	return sitter.Point{Row: uint(line), Column: uint(column)}, true
}
