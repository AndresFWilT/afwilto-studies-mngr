package domain

import "sort"

// LargestRemainder splits total into integer quotas proportional to shares:
// every quota starts at its floor, and the units left over go one by one to
// the largest fractional remainders. Ties go to the lowest index, which callers
// make the category order of first appearance. The quotas sum to total unless
// every share is zero, in which case all quotas are zero. Shares must be >= 0.
//
// It backs the inferred weights (AC-6) and, in slice 06a, the simulation draw
// (AC-14).
func LargestRemainder(total int, shares []int) []int {
	quotas := make([]int, len(shares))
	sum := 0
	for _, s := range shares {
		sum += s
	}
	if sum == 0 || total <= 0 {
		return quotas
	}

	remainders := make([]int, len(shares))
	assigned := 0
	for i, s := range shares {
		quotas[i] = total * s / sum
		remainders[i] = total * s % sum
		assigned += quotas[i]
	}

	order := make([]int, len(shares))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return remainders[order[a]] > remainders[order[b]] })
	for _, i := range order[:total-assigned] {
		quotas[i]++
	}
	return quotas
}
