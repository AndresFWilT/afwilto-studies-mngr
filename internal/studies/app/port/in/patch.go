package in

// Patch distinguishes an absent field (Set=false) from an explicit null
// (Set=true, Value=nil), keeping PATCH merge-patch semantics.
type Patch[T any] struct {
	Set   bool
	Value *T
}
