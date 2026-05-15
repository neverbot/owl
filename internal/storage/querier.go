package storage

// Querier is the narrow read-side interface for the query engine.
// It mirrors Store.Query without exposing the full Store surface.
type Querier interface {
	Query(metric string, from, to int64) ([]Series, error)
}

// Compile-time check that *Store satisfies Querier.
var _ Querier = (*Store)(nil)
