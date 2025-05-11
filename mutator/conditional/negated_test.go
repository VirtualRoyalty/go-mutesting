package conditional

import (
	"testing"

	"github.com/VirtualRoyalty/go-mutesting/test"
)

func TestMutatorConditionalNegated(t *testing.T) {
	test.Mutator(
		t,
		MutatorConditionalNegated,
		"../../testdata/conditional/negated.go",
		6,
	)
}
