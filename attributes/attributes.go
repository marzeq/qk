package attributes

type AttributeType string

const (
	AttributeTypeNoReturn AttributeType = "noreturn"
	AttributeTypeInline   AttributeType = "inline"
	AttributeTypeNoInline AttributeType = "noinline"
	AttributeTypeForeign  AttributeType = "foreign"
	AttributeTypeLink     AttributeType = "link"
	AttributeTypeExport   AttributeType = "export"
)

type Attribute interface {
	GetType() AttributeType // marker method
}

type AttributeNoReturn struct{}

func (a AttributeNoReturn) GetType() AttributeType { return AttributeTypeNoReturn }

type AttributeInline struct{}

func (a AttributeInline) GetType() AttributeType { return AttributeTypeInline }

type AttributeNoInline struct{}

func (a AttributeNoInline) GetType() AttributeType { return AttributeTypeNoInline }

type FunctionAttributeForeign struct {
	From string
}

func (a FunctionAttributeForeign) GetType() AttributeType { return AttributeTypeForeign }

type LinkKind uint8

const (
	LinkSystem LinkKind = iota
	LinkPath
	LinkSearchPath
	LinkFramework
)

type Link struct {
	Kind  LinkKind
	Value string
}

type ModuleAttributeLink struct {
	Links []Link
}

func (a ModuleAttributeLink) GetType() AttributeType { return AttributeTypeLink }

type FunctionAttributeExport struct {
	As string
}

func (a FunctionAttributeExport) GetType() AttributeType { return AttributeTypeExport }

type Attributes []Attribute

func (a Attributes) Get(attrType AttributeType) Attribute {
	for _, attr := range a {
		if attr.GetType() == attrType {
			return attr
		}
	}
	return nil
}
