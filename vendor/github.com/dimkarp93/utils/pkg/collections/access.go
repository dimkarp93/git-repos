package collections

func At[T any](items []T, i int) T {
	if i < 0 || i >= len(items) {
		var zero T
		return zero
	}

	return items[i]
}

func OrDefault[T any](items []T, def []T) []T {
	if len(items) == 0 {
		return def
	}

	return items
}
