package collections

import (
	"cmp"
	"maps"
	"slices"
)

func ToSet[T comparable](items []T) map[T]bool {
	set := make(map[T]bool, len(items))
	for _, item := range items {
		set[item] = true
	}

	return set
}

func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	return slices.Sorted(maps.Keys(m))
}

func CountBy[T any, K comparable](items []T, key func(T) K) map[K]int {
	out := make(map[K]int)
	for _, item := range items {
		out[key(item)]++
	}

	return out
}

func GroupBy[T any, K comparable](items []T, key func(T) K) map[K][]T {
	out := make(map[K][]T)
	for _, item := range items {
		k := key(item)
		out[k] = append(out[k], item)
	}

	return out
}
