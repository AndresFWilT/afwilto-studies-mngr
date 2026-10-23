package domain

import (
	"slices"
	"testing"
)

func TestLargestRemainderTable(t *testing.T) { // S03-2, AC-6, AC-14
	cases := []struct {
		name   string
		total  int
		shares []int
		want   []int
	}{
		{"AC-6: 5/3/3 questions", 100, []int{5, 3, 3}, []int{46, 27, 27}},
		{"AC-14: weights 30/26/24/20 over 65", 65, []int{30, 26, 24, 20}, []int{19, 17, 16, 13}},
		{"all-equal shares: the tie goes to the lowest index", 10, []int{1, 1, 1}, []int{4, 3, 3}},
		{"zero shares get nothing", 7, []int{0, 0, 5}, []int{0, 0, 7}},
		{"zero total", 0, []int{1, 2}, []int{0, 0}},
		{"all shares zero: nobody to give to", 100, []int{0, 0}, []int{0, 0}},
		{"exact division needs no remainder", 100, []int{1, 1, 1, 1}, []int{25, 25, 25, 25}},
		{"single share takes everything", 100, []int{7}, []int{100}},
		{"two tied remainders, one spare: lowest index wins", 100, []int{1, 1, 1}, []int{34, 33, 33}},
		{"empty shares", 100, []int{}, []int{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LargestRemainder(tc.total, tc.shares)
			if !slices.Equal(got, tc.want) {
				t.Errorf("LargestRemainder(%d, %v) = %v, want %v", tc.total, tc.shares, got, tc.want)
			}
		})
	}
}

func TestLargestRemainderAlwaysSumsToTheTotalWhenThereIsAnyShare(t *testing.T) { // property over a grid
	for total := 0; total <= 130; total += 13 {
		for _, shares := range [][]int{{1}, {1, 2}, {5, 3, 3}, {7, 7, 7, 7, 7, 7, 7}, {30, 26, 24, 20}, {0, 9, 1}, {1, 1000000}} {
			got := LargestRemainder(total, shares)
			sum := 0
			for _, q := range got {
				sum += q
			}
			if sum != total || len(got) != len(shares) {
				t.Errorf("LargestRemainder(%d, %v) = %v: sum %d, len %d", total, shares, got, sum, len(got))
			}
		}
	}
}

func TestLargestRemainderDoesNotMutateItsInput(t *testing.T) {
	shares := []int{5, 3, 3}
	LargestRemainder(100, shares)
	if !slices.Equal(shares, []int{5, 3, 3}) {
		t.Errorf("shares mutated: %v", shares)
	}
}
