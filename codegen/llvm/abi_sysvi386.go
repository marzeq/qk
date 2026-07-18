package llvm

import (
	"fmt"

	"github.com/marzeq/qk/types"
)

type sysVI386ABIGenerator struct{}

func (sysVI386ABIGenerator) aggregateParamChunks(e *Emitter, aggregate types.Type) []abiChunk {
	_, align := e.typeSizeAlign(aggregate)
	return []abiChunk{{
		typeName:   "ptr",
		attributes: fmt.Sprintf(" byval(%s) align %d", e.TypeEmit(aggregate), max(4, align)),
		offset:     -1,
	}}
}

func (sysVI386ABIGenerator) aggregateReturnChunks(*Emitter, types.Type) []abiChunk {
	return nil
}

func (sysVI386ABIGenerator) requiresSRet(*Emitter, types.Type) bool {
	return true
}
