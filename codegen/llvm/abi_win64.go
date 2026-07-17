package llvm

import (
	"fmt"

	"github.com/marzeq/qk/types"
)

type win64ABIGenerator struct{}

func (win64ABIGenerator) aggregateChunks(e *Emitter, st types.StructType) []abiChunk {
	size, _ := e.typeSizeAlign(st)
	switch size {
	case 1, 2, 4, 8:
		return []abiChunk{{typeName: fmt.Sprintf("i%d", size*8)}}
	default:
		return []abiChunk{{typeName: "ptr", offset: -1}}
	}
}

func (win64ABIGenerator) requiresSRet(e *Emitter, st types.StructType) bool {
	size, _ := e.typeSizeAlign(st)
	return size != 1 && size != 2 && size != 4 && size != 8
}
