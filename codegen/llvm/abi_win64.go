package llvm

import (
	"fmt"

	"github.com/marzeq/qk/types"
)

type win64ABIGenerator struct{}

func (win64ABIGenerator) aggregateChunks(e *Emitter, aggregate types.Type) []abiChunk {
	size, _ := e.typeSizeAlign(aggregate)
	switch size {
	case 1, 2, 4, 8:
		return []abiChunk{{typeName: fmt.Sprintf("i%d", size*8)}}
	default:
		return []abiChunk{{typeName: "ptr", offset: -1}}
	}
}

func (win64ABIGenerator) requiresSRet(e *Emitter, aggregate types.Type) bool {
	size, _ := e.typeSizeAlign(aggregate)
	return size != 1 && size != 2 && size != 4 && size != 8
}
