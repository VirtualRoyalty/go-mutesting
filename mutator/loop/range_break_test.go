package loop

import (
	"testing"

	"github.com/VirtualRoyalty/go-mutesting/test"
)

func TestMutatorLoopRangeBreak(t *testing.T) {
	test.Mutator(
		t,
		MutatorLoopRangeBreak,
		"../../testdata/loop/range_break.go",
		2,
	)
}
