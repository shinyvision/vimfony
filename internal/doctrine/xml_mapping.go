package doctrine

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

type xmlDoctrineMapping struct {
	Entities         []xmlEntity         `xml:"entity"`
	MappedSuperclass []xmlEntity         `xml:"mapped-superclass"`
	Embeddables      []xmlEmbeddableDecl `xml:"embeddable"`
}

type xmlEntity struct {
	Name       string            `xml:"name,attr"`
	IDs        []xmlID           `xml:"id"`
	Fields     []xmlField        `xml:"field"`
	ManyToOne  []xmlAssociation  `xml:"many-to-one"`
	OneToMany  []xmlAssociation  `xml:"one-to-many"`
	ManyToMany []xmlAssociation  `xml:"many-to-many"`
	OneToOne   []xmlAssociation  `xml:"one-to-one"`
	Embedded   []xmlEmbeddedDecl `xml:"embedded"`
}

type xmlEmbeddableDecl struct {
	Name   string     `xml:"name,attr"`
	Fields []xmlField `xml:"field"`
}

type xmlID struct {
	Name string `xml:"name,attr"`
}

type xmlField struct {
	Name string `xml:"name,attr"`
}

type xmlAssociation struct {
	Field        string `xml:"field,attr"`
	TargetEntity string `xml:"target-entity,attr"`
}

type xmlEmbeddedDecl struct {
	Name  string `xml:"name,attr"`
	Class string `xml:"class,attr"`
}

func parseXMLMappingFile(path string) ([]MappedField, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mapping xmlDoctrineMapping
	if err := xml.Unmarshal(data, &mapping); err != nil {
		return nil, err
	}

	var fields []MappedField
	for _, entity := range append(mapping.Entities, mapping.MappedSuperclass...) {
		fields = append(fields, fieldsFromXMLEntity(entity)...)
	}
	for _, emb := range mapping.Embeddables {
		for _, f := range emb.Fields {
			if f.Name != "" {
				fields = append(fields, MappedField{Name: f.Name, Kind: FieldKindColumn})
			}
		}
	}
	return fields, nil
}

func fieldsFromXMLEntity(entity xmlEntity) []MappedField {
	var fields []MappedField
	for _, id := range entity.IDs {
		if id.Name != "" {
			fields = append(fields, MappedField{Name: id.Name, Kind: FieldKindId})
		}
	}
	for _, f := range entity.Fields {
		if f.Name != "" {
			fields = append(fields, MappedField{Name: f.Name, Kind: FieldKindColumn})
		}
	}
	for _, a := range entity.ManyToOne {
		if a.Field != "" {
			fields = append(fields, MappedField{Name: a.Field, Kind: FieldKindAssociation, TargetEntity: normalizeXMLFQN(a.TargetEntity)})
		}
	}
	for _, a := range entity.OneToMany {
		if a.Field != "" {
			fields = append(fields, MappedField{Name: a.Field, Kind: FieldKindAssociation, TargetEntity: normalizeXMLFQN(a.TargetEntity)})
		}
	}
	for _, a := range entity.ManyToMany {
		if a.Field != "" {
			fields = append(fields, MappedField{Name: a.Field, Kind: FieldKindAssociation, TargetEntity: normalizeXMLFQN(a.TargetEntity)})
		}
	}
	for _, a := range entity.OneToOne {
		if a.Field != "" {
			fields = append(fields, MappedField{Name: a.Field, Kind: FieldKindAssociation, TargetEntity: normalizeXMLFQN(a.TargetEntity)})
		}
	}
	for _, e := range entity.Embedded {
		if e.Name != "" {
			fields = append(fields, MappedField{Name: e.Name, Kind: FieldKindEmbedded, TargetEntity: normalizeXMLFQN(e.Class)})
		}
	}
	return fields
}

func normalizeXMLFQN(fqn string) string {
	fqn = strings.TrimSpace(fqn)
	fqn = strings.TrimLeft(fqn, "\\")
	return fqn
}

func findXMLMappingFile(fqn, driverNamespace string, paths []string) string {
	ns := strings.TrimRight(driverNamespace, "\\")
	fqnClean := strings.TrimLeft(fqn, "\\")

	if !strings.HasPrefix(fqnClean, ns+"\\") && fqnClean != ns {
		return ""
	}

	relative := strings.TrimPrefix(fqnClean, ns+"\\")
	if relative == "" {
		return ""
	}
	fileName := strings.ReplaceAll(relative, "\\", ".") + ".orm.xml"

	for _, dir := range paths {
		candidate := filepath.Join(dir, fileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
