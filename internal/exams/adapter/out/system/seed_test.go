package system

import "testing"

func TestRandomSeedsCoverTheWholeUint64Range(t *testing.T) { // slice 06a §1.3: "defaults to a random uint64"
	seen := map[uint64]bool{}
	high := 0
	for range 256 {
		s := RandomSeeds{}.NewSeed()
		seen[s] = true
		if s > 1<<63 {
			high++
		}
	}
	if len(seen) < 255 {
		t.Errorf("only %d distinct seeds in 256 draws", len(seen))
	}
	if high < 64 || high > 192 {
		t.Errorf("%d of 256 seeds above 2^63, want about half: the top bit must be random too (the column stores the bit pattern)", high)
	}
}
