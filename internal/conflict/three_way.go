package conflict

import "reflect"

// Outcome captures deterministic three-way merge classification.
type Outcome string

const (
	OutcomeNoChange         Outcome = "no_change"
	OutcomeLocalChanged     Outcome = "local_changed"
	OutcomeRemoteChanged    Outcome = "remote_changed"
	OutcomeConvergedChanged Outcome = "converged_changed"
	OutcomeConflict         Outcome = "conflict"
)

// Comparison is the result of comparing local/original/remote values.
type Comparison[T any] struct {
	Outcome Outcome
	Base    T
	Local   T
	Remote  T
}

// EqualFunc compares values of type T.
type EqualFunc[T any] func(left, right T) bool

// Compare resolves a deterministic three-way merge outcome.
func Compare[T any](base, local, remote T, equal EqualFunc[T]) Comparison[T] {
	if equal == nil {
		equal = func(left, right T) bool {
			return reflect.DeepEqual(left, right)
		}
	}

	result := Comparison[T]{
		Base:   base,
		Local:  local,
		Remote: remote,
	}

	switch {
	case equal(base, local) && equal(base, remote):
		result.Outcome = OutcomeNoChange
	case equal(base, local) && !equal(base, remote):
		result.Outcome = OutcomeRemoteChanged
	case !equal(base, local) && equal(base, remote):
		result.Outcome = OutcomeLocalChanged
	case equal(local, remote):
		result.Outcome = OutcomeConvergedChanged
	default:
		result.Outcome = OutcomeConflict
	}

	return result
}

// CompareComparable resolves three-way merge outcomes for comparable types.
func CompareComparable[T comparable](base, local, remote T) Comparison[T] {
	return Compare(base, local, remote, func(left, right T) bool {
		return left == right
	})
}
