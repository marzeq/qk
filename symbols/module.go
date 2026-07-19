package symbols

type Module struct {
	Name                   string
	Scope                  *Scope
	TrustedStandardLibrary bool
}
