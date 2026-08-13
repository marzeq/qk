LLVM_PREFIX="$(brew --prefix llvm)"

export CGO_CXXFLAGS="-I${LLVM_PREFIX}/include"
export CGO_LDFLAGS="-L${LLVM_PREFIX}/lib"
