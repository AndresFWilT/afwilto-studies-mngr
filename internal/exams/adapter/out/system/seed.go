// Package system provides the production SeedSource of the exams context.
package system

import "math/rand/v2"

// RandomSeeds picks the seed of a draw when the caller does not. The
// top-level math/rand/v2 functions are seeded from the operating system.
type RandomSeeds struct{}

func (RandomSeeds) NewSeed() uint64 { return rand.Uint64() }
