package llvm

import (
	"fmt"

	"github.com/marzeq/qk/types"
)

type win32ABIGenerator struct{}

func (win32ABIGenerator) aggregateParamChunks(e *Emitter, aggregate types.Type) []abiChunk {
	size, _ := e.typeSizeAlign(aggregate)
	switch size {
	case 4:
		return []abiChunk{{typeName: "i32"}}
	case 8:
		return []abiChunk{{typeName: "i32"}, {typeName: "i32", offset: 4}}
	default:
		return []abiChunk{{
			typeName:   "ptr",
			attributes: fmt.Sprintf(" byval(%s) align 4", e.TypeEmit(aggregate)),
			offset:     -1,
		}}
	}
}

func (win32ABIGenerator) aggregateReturnChunks(e *Emitter, aggregate types.Type) []abiChunk {
	size, _ := e.typeSizeAlign(aggregate)
	switch size {
	case 1, 2, 4, 8:
		return []abiChunk{{typeName: fmt.Sprintf("i%d", size*8)}}
	default:
		return nil
	}
}

func (win32ABIGenerator) requiresSRet(e *Emitter, aggregate types.Type) bool {
	size, _ := e.typeSizeAlign(aggregate)
	return size != 1 && size != 2 && size != 4 && size != 8
}
