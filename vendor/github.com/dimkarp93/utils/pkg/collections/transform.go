package collections

func Filter[T any](items []T, keep func(T) bool) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		if keep(item) {
			out = append(out, item)
		}
	}

	return out
}

func Map[T, R any](items []T, fn func(T) R) []R {
	out := make([]R, 0, len(items))
	for _, item := range items {
		out = append(out, fn(item))
	}

	return out
}

func FlatMap[T, R any](items []T, fn func(T) []R) []R {
	out := make([]R, 0, len(items))
	for _, item := range items {
		out = append(out, fn(item)...)
	}

	return out
}

func CountFunc[T any](items []T, keep func(T) bool) int {
	count := 0
	for _, item := range items {
		if keep(item) {
			count++
		}
	}

	return count
}
