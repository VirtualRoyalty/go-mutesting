package arithmetic

import (
	"testing"

	"github.com/VirtualRoyalty/go-mutesting/test"
)

func TestMutatorArithmeticAssignment(t *testing.T) {
	test.Mutator(
		t,
		MutatorArithmeticAssignment,
		"../../testdata/arithmetic/assignment.go",
		11,
	)
}
