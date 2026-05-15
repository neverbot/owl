package storage

// Appender is the narrow surface that collectors and scrapers depend on
// for writing samples. Decoupling the writer side from the full *Store
// keeps collectors easy to test with a fake.
type Appender interface {
	Append(samples []Sample) error
}

// Compile-time check that *Store satisfies Appender.
var _ Appender = (*Store)(nil)
