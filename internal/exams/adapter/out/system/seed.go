// Package system provides the production SeedSource of the exams context.
package system

// RandomSeeds picks the seed of a draw when the caller does not.
type RandomSeeds struct{}

func (RandomSeeds) NewSeed() uint64 { return 0 }
