package doctrine

type FieldKind string

const (
	FieldKindId          FieldKind = "id"
	FieldKindColumn      FieldKind = "column"
	FieldKindAssociation FieldKind = "association"
	FieldKindEmbedded    FieldKind = "embedded"
)

type MappedField struct {
	Name         string
	Kind         FieldKind
	TargetEntity string
}
