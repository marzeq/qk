//go:build cgo && llvm_static

package llvmbackend

import "C"

// Static release builds deliberately have no built-in library list. LLVM's
// static archive closure varies with its configured targets and optional
// dependencies, so the release toolchain must provide it through CGO_LDFLAGS.
// Keeping those flags outside the source also permits absolute archive paths,
// which prevent the external linker from selecting a shared library.
