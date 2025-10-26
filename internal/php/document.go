package php

import (
	"context"
	"sort"
	"sync"
	"time"

	phpforest "github.com/alexaandru/go-sitter-forest/php"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

const analysisDebounceInterval = 500 * time.Millisecond

// Document maintains a parsed PHP syntax tree together with its static analysis index.
// It owns the tree-sitter parser and decides when static analysis should be re-run.
type Document struct {
	parser          *sitter.Parser
	mu              sync.RWMutex
	tree            *sitter.Tree
	content         []byte
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

// Update notifies the document about new file contents. If change is nil, the file
// has been replaced entirely. Incremental edits can be provided via change.
func (d *Document) Update(code []byte, change *sitter.InputEdit) error {
	d.mu.Lock()

	d.content = code
	if d.tree != nil && change != nil {
		d.tree.Edit(*change)
	}

	newTree, err := d.parser.ParseString(context.Background(), d.tree, code)
	if err != nil {
		d.mu.Unlock()
		return err
	}

	if d.tree != nil {
		d.tree.Close()
	}
	d.tree = newTree

	version := d.analysisVersion + 1
	d.analysisVersion = version

	if change == nil {
		d.dirtyRanges = nil
		if d.analysisTimer != nil {
			d.analysisTimer.Stop()
			d.analysisTimer = nil
		}
	} else {
		d.recordDirtyRangeLocked(change)
	}

	immediate := change == nil

	if !immediate {
		if d.analysisTimer != nil {
			d.analysisTimer.Stop()
		}
		d.analysisTimer = time.AfterFunc(analysisDebounceInterval, func() {
			d.runAnalysis(version)
		})
	}

	d.mu.Unlock()

	if immediate {
		d.runAnalysis(version)
	}

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

func (d *Document) runAnalysis(version int64) {
	d.mu.RLock()
	if d.analysisVersion != version || d.tree == nil {
		d.mu.RUnlock()
		return
	}
	treeCopy := d.tree.Copy()
	dirty := append([]ByteRange(nil), d.dirtyRanges...)
	contentCopy := append([]byte(nil), d.content...)
	analyzer := d.analyzer
	d.mu.RUnlock()

	if analyzer == nil || treeCopy == nil {
		if treeCopy != nil {
			treeCopy.Close()
		}
		return
	}
	defer treeCopy.Close()

	index := analyzer.Update(&contentCopy, treeCopy, dirty)

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.analysisVersion != version {
		return
	}
	d.index = index
	d.lastAnalyzed = version
	d.dirtyRanges = nil
	if d.analysisTimer != nil {
		d.analysisTimer.Stop()
		d.analysisTimer = nil
	}
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
