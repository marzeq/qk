LLVM_PREFIX="$(brew --prefix llvm@22)"

export CC="${LLVM_PREFIX}/bin/clang"
export CXX="${LLVM_PREFIX}/bin/clang++"
export CGO_ENABLED=1
export CGO_CXXFLAGS="-I${LLVM_PREFIX}/include"
export CGO_LDFLAGS="-L${LLVM_PREFIX}/lib"
