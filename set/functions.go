package set

import "sort"

type (
	comparable interface {
		string | int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64 | float32 | float64
	}
)

func With[T comparable](incoming ...T) []T {
	return Set(incoming)
}

func Set[T comparable](incoming []T) []T {
	return Keys(itemset(incoming))
}

func Values[Key, Value comparable](incoming map[Key]Value) []Value {
	intermediate := make(map[Value]bool)
	for _, value := range incoming {
		intermediate[value] = true
	}
	return Keys(intermediate)
}

func Keys[Key comparable, Value any](incoming map[Key]Value) []Key {
	result := make([]Key, 0, len(incoming))
	for key, _ := range incoming {
		result = append(result, key)
	}
	return Sort(result)
}

func Sort[T comparable](set []T) []T {
	sort.Slice(set, func(left, right int) bool {
		return set[left] < set[right]
	})
	return set
}

func Member[T comparable](set []T, candidate T) bool {
	for _, item := range set {
		if candidate == item {
			return true
		}
	}
	return false
}

func Membership[T comparable](set []T) map[T]bool {
	result := make(map[T]bool)
	for _, item := range set {
		result[item] = true
	}
	return result
}

func Update[T comparable](set []T, candidate T) ([]T, bool) {
	if Member(set, candidate) {
		return set, false
	}
	return Sort(append(set, candidate)), true
}

func Intersect[T comparable](left, right []T) []T {
	if len(right) < len(left) {
		left, right = right, left
	}
	missing := len(left)
	checked := itemset(left)
	intermediate := make(map[T]bool)
	for _, candidate := range right {
		if checked[candidate] {
			intermediate[candidate] = true
			missing--
		}
		if missing == 0 {
			break
		}
	}
	return Keys(intermediate)
}

func Union[T comparable](left, right []T) []T {
	intermediate := itemset(left)
	for _, item := range right {
		intermediate[item] = true
	}
	return Keys(intermediate)
}

func itemset[T comparable](items []T) map[T]bool {
	result := make(map[T]bool)
	for _, item := range items {
		result[item] = true
	}
	return result
}
