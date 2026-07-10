package attributes

type AttributeType string
const (
	AttributeTypeNoReturn AttributeType = "noreturn"
	AttributeTypeInline   AttributeType = "inline"
	AttributeTypeNoInline AttributeType = "noinline"
	AttributeTypeForeign  AttributeType = "foreign"
)

type Attribute interface {
	GetType() AttributeType // marker method
}

type FunctionAttributeNoReturn struct{}
func (a FunctionAttributeNoReturn) GetType() AttributeType { return AttributeTypeNoReturn }
type FunctionAttributeInline struct{}
func (a FunctionAttributeInline) GetType() AttributeType { return AttributeTypeInline }
type FunctionAttributeNoInline struct{}
func (a FunctionAttributeNoInline) GetType() AttributeType { return AttributeTypeNoInline }
type FunctionAttributeForeign struct {
	From string
}
func (a FunctionAttributeForeign) GetType() AttributeType { return AttributeTypeForeign }

type Attributes []Attribute

func (a Attributes) Get(attrType AttributeType) Attribute {
	for _, attr := range a {
		if attr.GetType() == attrType {
			return attr
		}
	}
	return nil
}
