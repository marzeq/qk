LLVM_PREFIX="$(brew --prefix llvm)"
LLD_PREFIX="$(brew --prefix lld)"

export CGO_CXXFLAGS="-I${LLVM_PREFIX}/include -I${LLD_PREFIX}/include"
export CGO_LDFLAGS="-L${LLVM_PREFIX}/lib -L${LLD_PREFIX}/lib"
