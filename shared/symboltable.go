package shared

type SymbolTable[T any] struct {
	scopes []map[string]T
}

func NewSymbolTable[T any](_initial ...map[string]T) *SymbolTable[T] {
	initial := map[string]T{}
	if len(_initial) > 0 {
		initial = _initial[0]
	}
	return &SymbolTable[T]{scopes: []map[string]T{initial}}
}

func (st *SymbolTable[T]) EnterScope() {
	st.scopes = append(st.scopes, make(map[string]T))
}

func (st *SymbolTable[T]) ExitScope() {
	if len(st.scopes) > 1 {
		st.scopes = st.scopes[:len(st.scopes)-1]
	} else {
		panic("cannot exit global scope")
	}
}

func (st *SymbolTable[T]) GetScope() map[string]T {
	return st.scopes[len(st.scopes)-1]
}

func (st *SymbolTable[T]) Define(name string, value T) bool {
	currentScope := st.scopes[len(st.scopes)-1]
	if _, ok := st.Lookup(name); ok {
		return false
	}
	currentScope[name] = value

	return true
}

func (st *SymbolTable[T]) Lookup(name string) (T, bool) {
	for i := len(st.scopes) - 1; i >= 0; i-- {
		if value, found := st.scopes[i][name]; found {
			return value, true
		}
	}
	var zeroVal T
	return zeroVal, false
}
