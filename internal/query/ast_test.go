package query

import "testing"

// TestASTTypes verifies that all node types exist and satisfy the Node interface.
func TestASTTypes(t *testing.T) {
	var _ Node = &SelectorNode{}
	var _ Node = &RateNode{}
	var _ Node = &AggregationNode{}
	var _ Node = &BinaryOpNode{}
}
