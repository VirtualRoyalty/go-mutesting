package branch

import (
	"testing"

	"github.com/VirtualRoyalty/go-mutesting/test"
)

func TestMutatorCase(t *testing.T) {
	test.Mutator(
		t,
		MutatorCase,
		"../../testdata/branch/mutatecase.go",
		3,
	)
}
