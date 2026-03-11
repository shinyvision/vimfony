package doctrine

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

var ormMappingAttributes = map[string]FieldKind{
	"Id":         FieldKindId,
	"Column":     FieldKindColumn,
	"ManyToOne":  FieldKindAssociation,
	"OneToMany":  FieldKindAssociation,
	"ManyToMany": FieldKindAssociation,
	"OneToOne":   FieldKindAssociation,
	"Embedded":   FieldKindEmbedded,
}

var ormAssociationAttributes = map[string]bool{
	"ManyToOne":  true,
	"OneToMany":  true,
	"ManyToMany": true,
	"OneToOne":   true,
	"Embedded":   true,
}

func extractAttributeFields(root sitter.Node, content []byte, ormAlias string) []MappedField {
	if root.IsNull() {
		return nil
	}
	if ormAlias == "" {
		ormAlias = "ORM"
	}

	uses := collectUseMap(root, content)
	namespace := detectNamespace(root, content)

	var fields []MappedField
	walkNodes(root, func(node sitter.Node) {
		if node.Type() != "property_declaration" {
			return
		}
		kind, targetEntity, ok := propertyORMMapping(node, content, ormAlias, uses, namespace)
		if !ok {
			return
		}
		for _, name := range propertyNames(node, content) {
			fields = append(fields, MappedField{Name: name, Kind: kind, TargetEntity: targetEntity})
		}
	})
	return fields
}

func propertyORMMapping(propNode sitter.Node, content []byte, ormAlias string, uses map[string]string, namespace string) (FieldKind, string, bool) {
	found := false
	bestKind := FieldKindColumn
	var targetEntity string

	walkNodes(propNode, func(node sitter.Node) {
		if node.Type() != "attribute" {
			return
		}
		attrName := attributeName(node, content)
		if attrName == "" {
			return
		}
		kind, short, ok := matchORMAttributeWithShort(attrName, ormAlias)
		if !ok {
			return
		}
		found = true
		if kindPriority(kind) > kindPriority(bestKind) {
			bestKind = kind
		}
		if ormAssociationAttributes[short] {
			if te := extractTargetEntity(node, content, uses, namespace); te != "" {
				targetEntity = te
			}
		}
	})

	return bestKind, targetEntity, found
}

func matchORMAttributeWithShort(name, ormAlias string) (FieldKind, string, bool) {
	prefix := ormAlias + "\\"
	if strings.HasPrefix(name, prefix) {
		short := name[len(prefix):]
		if kind, ok := ormMappingAttributes[short]; ok {
			return kind, short, true
		}
	}
	if strings.HasPrefix(name, "Doctrine\\ORM\\Mapping\\") {
		short := name[len("Doctrine\\ORM\\Mapping\\"):]
		if kind, ok := ormMappingAttributes[short]; ok {
			return kind, short, true
		}
	}
	return "", "", false
}

func extractTargetEntity(attrNode sitter.Node, content []byte, uses map[string]string, namespace string) string {
	var argsNode sitter.Node
	for i := uint32(0); i < attrNode.NamedChildCount(); i++ {
		child := attrNode.NamedChild(i)
		if child.Type() == "arguments" {
			argsNode = child
			break
		}
	}
	if argsNode.IsNull() {
		return ""
	}

	for i := uint32(0); i < argsNode.NamedChildCount(); i++ {
		arg := argsNode.NamedChild(i)
		if arg.Type() != "argument" {
			continue
		}

		nameNode := arg.ChildByFieldName("name")
		if !nameNode.IsNull() {
			argName := strings.TrimSpace(nameNode.Content(content))
			if argName == "targetEntity" {
				return classConstantToFQN(arg, content, uses, namespace)
			}
			continue
		}

		if i == 0 {
			if fqn := classConstantToFQN(arg, content, uses, namespace); fqn != "" {
				return fqn
			}
		}
	}
	return ""
}

func classConstantToFQN(argNode sitter.Node, content []byte, uses map[string]string, namespace string) string {
	var result string
	walkNodes(argNode, func(node sitter.Node) {
		if result != "" {
			return
		}
		if node.Type() != "class_constant_access_expression" {
			return
		}
		if node.NamedChildCount() < 2 {
			return
		}
		constNode := node.NamedChild(node.NamedChildCount() - 1)
		if strings.TrimSpace(constNode.Content(content)) != "class" {
			return
		}
		classNode := node.NamedChild(0)
		raw := strings.TrimSpace(classNode.Content(content))
		if raw != "" {
			result = resolveClassName(raw, uses, namespace)
		}
	})
	return result
}

func collectUseMap(root sitter.Node, content []byte) map[string]string {
	uses := make(map[string]string)
	walkNodes(root, func(node sitter.Node) {
		if node.Type() != "namespace_use_declaration" {
			return
		}
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child.Type() == "namespace_use_clause" {
				addUseClause(child, content, uses)
			}
			if child.Type() == "namespace_use_group" {
				var prefix string
				for j := uint32(0); j < node.NamedChildCount(); j++ {
					nn := node.NamedChild(j)
					if nn.Type() == "namespace_name" {
						prefix = strings.TrimSpace(nn.Content(content))
						break
					}
				}
				for j := uint32(0); j < child.NamedChildCount(); j++ {
					gc := child.NamedChild(j)
					if gc.Type() == "namespace_use_clause" {
						addUseClauseWithPrefix(gc, prefix, content, uses)
					}
				}
			}
		}
	})
	return uses
}

func addUseClause(clause sitter.Node, content []byte, uses map[string]string) {
	addUseClauseWithPrefix(clause, "", content, uses)
}

func addUseClauseWithPrefix(clause sitter.Node, prefix string, content []byte, uses map[string]string) {
	var nameStr, aliasStr string
	for i := uint32(0); i < clause.NamedChildCount(); i++ {
		child := clause.NamedChild(i)
		if clause.FieldNameForNamedChild(i) == "alias" {
			aliasStr = strings.TrimSpace(child.Content(content))
			continue
		}
		switch child.Type() {
		case "qualified_name", "name", "namespace_name":
			nameStr = strings.TrimSpace(child.Content(content))
		}
	}
	if nameStr == "" {
		return
	}
	full := nameStr
	if prefix != "" {
		full = prefix + "\\" + strings.TrimLeft(nameStr, "\\")
	}
	full = strings.TrimLeft(full, "\\")
	if aliasStr == "" {
		if idx := strings.LastIndex(full, "\\"); idx >= 0 {
			aliasStr = full[idx+1:]
		} else {
			aliasStr = full
		}
	}
	uses[strings.ToLower(aliasStr)] = full
}

func detectNamespace(root sitter.Node, content []byte) string {
	var ns string
	walkNodes(root, func(node sitter.Node) {
		if ns != "" {
			return
		}
		if node.Type() == "namespace_definition" || node.Type() == "namespace_declaration" {
			nameNode := node.ChildByFieldName("name")
			if !nameNode.IsNull() {
				ns = strings.TrimSpace(nameNode.Content(content))
				ns = strings.TrimLeft(ns, "\\")
			}
		}
	})
	return ns
}

func kindPriority(k FieldKind) int {
	switch k {
	case FieldKindId:
		return 3
	case FieldKindAssociation:
		return 2
	case FieldKindEmbedded:
		return 2
	case FieldKindColumn:
		return 1
	default:
		return 0
	}
}

func attributeName(node sitter.Node, content []byte) string {
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		switch child.Type() {
		case "name", "qualified_name":
			return strings.TrimSpace(child.Content(content))
		}
	}
	return ""
}

func propertyNames(propNode sitter.Node, content []byte) []string {
	var names []string
	for i := uint32(0); i < propNode.NamedChildCount(); i++ {
		child := propNode.NamedChild(i)
		if child.Type() != "property_element" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode.IsNull() {
			continue
		}
		raw := strings.TrimSpace(nameNode.Content(content))
		if nameNode.Type() == "variable_name" {
			for j := uint32(0); j < nameNode.NamedChildCount(); j++ {
				inner := nameNode.NamedChild(j)
				if inner.Type() == "name" {
					raw = strings.TrimSpace(inner.Content(content))
					break
				}
			}
		}
		name := strings.TrimPrefix(raw, "$")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func resolveORMAlias(root sitter.Node, content []byte) string {
	if root.IsNull() {
		return ""
	}
	var alias string
	walkNodes(root, func(node sitter.Node) {
		if alias != "" {
			return
		}
		if node.Type() != "namespace_use_declaration" {
			return
		}
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child.Type() == "namespace_use_clause" {
				a := checkUseClauseForORM(child, content)
				if a != "" {
					alias = a
					return
				}
			}
			if child.Type() == "namespace_use_group" {
				for j := uint32(0); j < child.NamedChildCount(); j++ {
					gc := child.NamedChild(j)
					if gc.Type() == "namespace_use_clause" {
						a := checkUseClauseForORM(gc, content)
						if a != "" {
							alias = a
							return
						}
					}
				}
			}
		}
	})
	return alias
}

func checkUseClauseForORM(clause sitter.Node, content []byte) string {
	var nameStr string
	var aliasStr string

	for i := uint32(0); i < clause.NamedChildCount(); i++ {
		child := clause.NamedChild(i)
		if clause.FieldNameForNamedChild(i) == "alias" {
			aliasStr = strings.TrimSpace(child.Content(content))
			continue
		}
		switch child.Type() {
		case "qualified_name", "name":
			nameStr = strings.TrimSpace(child.Content(content))
		}
	}

	nameStr = strings.TrimLeft(nameStr, "\\")
	nameStr = strings.ReplaceAll(nameStr, "\\\\", "\\")

	if nameStr == "Doctrine\\ORM\\Mapping" {
		if aliasStr != "" {
			return aliasStr
		}
		return "Mapping"
	}
	return ""
}

func extractTraitUses(classNode sitter.Node, content []byte, uses map[string]string, namespace string) []string {
	if classNode.IsNull() {
		return nil
	}

	var traits []string
	for i := uint32(0); i < classNode.NamedChildCount(); i++ {
		body := classNode.NamedChild(i)
		if body.Type() != "declaration_list" {
			continue
		}
		for j := uint32(0); j < body.NamedChildCount(); j++ {
			child := body.NamedChild(j)
			if child.Type() != "use_declaration" {
				continue
			}
			// Each use_declaration can reference multiple traits
			for k := uint32(0); k < child.NamedChildCount(); k++ {
				nameNode := child.NamedChild(k)
				switch nameNode.Type() {
				case "name", "qualified_name":
					raw := strings.TrimSpace(nameNode.Content(content))
					fqn := resolveClassName(raw, uses, namespace)
					if fqn != "" {
						traits = append(traits, fqn)
					}
				}
			}
		}
	}
	return traits
}

// resolveClassName resolves a short or partially-qualified class name to FQN
// using the use-map and current namespace.
func resolveClassName(name string, uses map[string]string, namespace string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimLeft(name, "\\")
	if name == "" {
		return ""
	}

	lower := strings.ToLower(name)
	if full, ok := uses[lower]; ok {
		return full
	}
	parts := strings.SplitN(name, "\\", 2)
	shortLower := strings.ToLower(parts[0])
	if full, ok := uses[shortLower]; ok {
		if len(parts) == 2 {
			return full + "\\" + parts[1]
		}
		return full
	}

	if strings.Contains(name, "\\") {
		return name
	}

	if namespace != "" {
		return namespace + "\\" + name
	}
	return name
}

// walkNodes is a simple depth-first AST walker.
func walkNodes(node sitter.Node, fn func(sitter.Node)) {
	if node.IsNull() {
		return
	}
	fn(node)
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		walkNodes(node.NamedChild(i), fn)
	}
}
