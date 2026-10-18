package collections

import (
	"cmp"
	"slices"
)

func Unique[T comparable](items []T) []T {
	return UniqueBy(items, identity)
}

func UniqueBy[T any, K comparable](items []T, key func(T) K) []T {
	seen := make(map[K]struct{}, len(items))
	out := make([]T, 0, len(items))
	for _, item := range items {
		k := key(item)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, item)
		}
	}

	return out
}

func SortedUnique[T cmp.Ordered](items []T) []T {
	out := slices.Clone(items)
	slices.Sort(out)

	return slices.Compact(out)
}

func identity[T any](item T) T {
	return item
}
