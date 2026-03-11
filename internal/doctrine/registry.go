package doctrine

import (
	"os"
	"strings"
	"sync"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/php"
)

type Registry struct {
	mu                    sync.RWMutex
	drivers               []config.DoctrineDriverMapping
	autoload              config.AutoloadMap
	root                  string
	store                 *php.DocumentStore
	resolveTargetEntities map[string]string
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Configure(
	drivers []config.DoctrineDriverMapping,
	autoload config.AutoloadMap,
	root string,
	store *php.DocumentStore,
	resolveTargetEntities map[string]string,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drivers = drivers
	r.autoload = autoload
	r.root = root
	r.store = store
	r.resolveTargetEntities = resolveTargetEntities
}

func (r *Registry) MappedFields(fqn string) []MappedField {
	r.mu.RLock()
	drivers := r.drivers
	autoload := r.autoload
	root := r.root
	store := r.store
	rte := r.resolveTargetEntities
	r.mu.RUnlock()

	fqn = normalizeFQN(fqn)
	if fqn == "" {
		return nil
	}

	if resolved, ok := rte[fqn]; ok {
		fqn = normalizeFQN(resolved)
	}

	ctx := &resolveContext{
		drivers:  drivers,
		autoload: autoload,
		root:     root,
		store:    store,
		visited:  make(map[string]bool),
	}

	return ctx.resolve(fqn)
}

func (r *Registry) AssociationTargetEntity(entityFQN, fieldName string) string {
	r.mu.RLock()
	rte := r.resolveTargetEntities
	r.mu.RUnlock()

	fields := r.MappedFields(entityFQN)
	for _, f := range fields {
		if f.Name == fieldName && f.TargetEntity != "" {
			target := normalizeFQN(f.TargetEntity)
			if resolved, ok := rte[target]; ok {
				return resolved
			}
			return f.TargetEntity
		}
	}
	return ""
}

func (r *Registry) IsMapped(fqn string) bool {
	r.mu.RLock()
	drivers := r.drivers
	r.mu.RUnlock()

	fqn = normalizeFQN(fqn)
	if fqn == "" {
		return false
	}

	for _, d := range drivers {
		ns := strings.TrimRight(d.Namespace, "\\")
		if strings.HasPrefix(fqn, ns+"\\") || fqn == ns {
			return true
		}
	}
	return false
}

type resolveContext struct {
	drivers  []config.DoctrineDriverMapping
	autoload config.AutoloadMap
	root     string
	store    *php.DocumentStore
	visited  map[string]bool
}

func (ctx *resolveContext) resolve(fqn string) []MappedField {
	fqn = normalizeFQN(fqn)
	if fqn == "" {
		return nil
	}

	key := strings.ToLower(fqn)
	if ctx.visited[key] {
		return nil
	}
	ctx.visited[key] = true

	own := ctx.ownFields(fqn)

	var extends []string
	var traits []string
	doc, path := ctx.loadDocument(fqn)
	if doc != nil {
		doc.Read(func(tree *sitter.Tree, content []byte, index php.IndexedTree) {
			if tree == nil {
				return
			}
			root := tree.RootNode()
			ormAlias := resolveORMAlias(root, content)

			classNode := findClassNode(root, content)
			if classNode.IsNull() {
				return
			}

			for _, cls := range index.Classes {
				extends = cls.Extends
				break
			}

			traits = extractTraitUses(classNode, content, index.Uses, namespaceFromIndex(index))

			if len(own) == 0 && ormAlias != "" {
				own = extractAttributeFields(root, content, ormAlias)
			}
		})

		if len(own) == 0 {
			own = ctx.attributeFieldsFromFile(path)
		}
	}

	fieldMap := make(map[string]MappedField)
	for _, parentFQN := range extends {
		for _, f := range ctx.resolve(parentFQN) {
			fieldMap[f.Name] = f
		}
	}

	for _, traitFQN := range traits {
		for _, f := range ctx.resolveTraitFields(traitFQN) {
			fieldMap[f.Name] = f
		}
	}

	for _, f := range own {
		if existing, ok := fieldMap[f.Name]; ok && f.TargetEntity == "" && existing.TargetEntity != "" {
			f.TargetEntity = existing.TargetEntity
		}
		fieldMap[f.Name] = f
	}

	if len(fieldMap) == 0 {
		return nil
	}

	result := make([]MappedField, 0, len(fieldMap))
	for _, f := range fieldMap {
		result = append(result, f)
	}
	return result
}

func (ctx *resolveContext) resolveTraitFields(fqn string) []MappedField {
	fqn = normalizeFQN(fqn)
	if fqn == "" {
		return nil
	}

	key := strings.ToLower(fqn)
	if ctx.visited[key] {
		return nil
	}
	ctx.visited[key] = true

	fields := ctx.attributeFieldsFromFQN(fqn)

	doc, _ := ctx.loadDocument(fqn)
	if doc != nil {
		doc.Read(func(tree *sitter.Tree, content []byte, index php.IndexedTree) {
			if tree == nil {
				return
			}
			root := tree.RootNode()
			traitNode := findTraitNode(root, content)
			if traitNode.IsNull() {
				return
			}
			subTraits := extractTraitUses(traitNode, content, index.Uses, namespaceFromIndex(index))
			for _, st := range subTraits {
				for _, f := range ctx.resolveTraitFields(st) {
					fields = append(fields, f)
				}
			}
		})
	}

	return fields
}

func (ctx *resolveContext) ownFields(fqn string) []MappedField {
	drivers := ctx.findDrivers(fqn)
	for _, driver := range drivers {
		var fields []MappedField
		switch driver.Kind {
		case config.DriverKindAttribute:
			fields = ctx.attributeFieldsFromFQN(fqn)
		case config.DriverKindXML:
			fields = ctx.xmlFields(fqn, driver)
		}
		if len(fields) > 0 {
			return fields
		}
	}
	return nil
}

func (ctx *resolveContext) findDrivers(fqn string) []*config.DoctrineDriverMapping {
	type candidate struct {
		driver *config.DoctrineDriverMapping
		nsLen  int
	}
	var candidates []candidate

	for i := range ctx.drivers {
		d := &ctx.drivers[i]
		ns := strings.TrimRight(d.Namespace, "\\")
		prefix := ns + "\\"
		if strings.HasPrefix(fqn, prefix) || fqn == ns {
			candidates = append(candidates, candidate{driver: d, nsLen: len(ns)})
		}
	}

	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].nsLen > candidates[j-1].nsLen; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}

	result := make([]*config.DoctrineDriverMapping, len(candidates))
	for i, c := range candidates {
		result[i] = c.driver
	}
	return result
}

func (ctx *resolveContext) xmlFields(fqn string, driver *config.DoctrineDriverMapping) []MappedField {
	xmlPath := findXMLMappingFile(fqn, driver.Namespace, driver.Paths)
	if xmlPath == "" {
		return nil
	}
	fields, err := parseXMLMappingFile(xmlPath)
	if err != nil {
		return nil
	}
	return fields
}

func (ctx *resolveContext) attributeFieldsFromFQN(fqn string) []MappedField {
	_, path := ctx.loadDocument(fqn)
	if path == "" {
		return nil
	}
	return ctx.attributeFieldsFromFile(path)
}

func (ctx *resolveContext) attributeFieldsFromFile(path string) []MappedField {
	if path == "" || ctx.store == nil {
		return nil
	}
	doc, err := ctx.store.Get(path)
	if err != nil || doc == nil {
		return nil
	}

	var fields []MappedField
	doc.Read(func(tree *sitter.Tree, content []byte, _ php.IndexedTree) {
		if tree == nil {
			return
		}
		root := tree.RootNode()
		ormAlias := resolveORMAlias(root, content)
		if ormAlias == "" {
			return
		}
		fields = extractAttributeFields(root, content, ormAlias)
	})
	return fields
}

func (ctx *resolveContext) loadDocument(fqn string) (*php.Document, string) {
	if ctx.store == nil {
		return nil, ""
	}

	path, ok := config.AutoloadResolve(fqn, ctx.autoload, ctx.root)
	if !ok || path == "" {
		return nil, ""
	}

	if _, err := os.Stat(path); err != nil {
		return nil, ""
	}

	doc, err := ctx.store.Get(path)
	if err != nil {
		return nil, path
	}
	return doc, path
}

func findClassNode(root sitter.Node, _ []byte) sitter.Node {
	if root.IsNull() {
		return sitter.Node{}
	}
	var result sitter.Node
	walkNodesUntil(root, func(node sitter.Node) bool {
		if node.Type() == "class_declaration" {
			result = node
			return false
		}
		return true
	})
	return result
}

func findTraitNode(root sitter.Node, _ []byte) sitter.Node {
	if root.IsNull() {
		return sitter.Node{}
	}
	var result sitter.Node
	walkNodesUntil(root, func(node sitter.Node) bool {
		if node.Type() == "trait_declaration" {
			result = node
			return false
		}
		return true
	})
	return result
}

func walkNodesUntil(node sitter.Node, fn func(sitter.Node) bool) bool {
	if node.IsNull() {
		return true
	}
	if !fn(node) {
		return false
	}
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		if !walkNodesUntil(node.NamedChild(i), fn) {
			return false
		}
	}
	return true
}

func namespaceFromIndex(index php.IndexedTree) string {
	for _, cls := range index.Classes {
		return cls.Namespace
	}
	return ""
}

func normalizeFQN(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\\\", "\\")
	name = strings.TrimLeft(name, "\\")
	return name
}

