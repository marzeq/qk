package symbols

import "fmt"

type Scope struct {
	Parent  *Scope
	Symbols map[string]*Symbol
}

func NewScope(parent *Scope) *Scope {
	return &Scope{
		Parent:  parent,
		Symbols: make(map[string]*Symbol),
	}
}

func (s *Scope) NewChild() *Scope {
	return NewScope(s)
}
func (s *Scope) Define(sym *Symbol) error {
	if _, exists := s.Symbols[sym.Name]; exists {
		return fmt.Errorf("symbol '%v' already defined in this scope", sym.Name)
	}

	s.Symbols[sym.Name] = sym
	return nil
}

func (s *Scope) Resolve(name string) (*Symbol, bool) {
	for sc := s; sc != nil; sc = sc.Parent {
		if sym, ok := sc.Symbols[name]; ok {
			return sym, true
		}
	}
	return nil, false
}
