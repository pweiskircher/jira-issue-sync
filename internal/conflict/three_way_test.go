package conflict

import (
	"reflect"
	"testing"
)

func TestCompareComparableOutcomes(t *testing.T) {
	testCases := []struct {
		name     string
		base     string
		local    string
		remote   string
		expected Outcome
	}{
		{name: "no change", base: "a", local: "a", remote: "a", expected: OutcomeNoChange},
		{name: "local changed", base: "a", local: "b", remote: "a", expected: OutcomeLocalChanged},
		{name: "remote changed", base: "a", local: "a", remote: "b", expected: OutcomeRemoteChanged},
		{name: "converged changed", base: "a", local: "b", remote: "b", expected: OutcomeConvergedChanged},
		{name: "conflict", base: "a", local: "b", remote: "c", expected: OutcomeConflict},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			comparison := CompareComparable(testCase.base, testCase.local, testCase.remote)
			if comparison.Outcome != testCase.expected {
				t.Fatalf("unexpected outcome: got=%s want=%s", comparison.Outcome, testCase.expected)
			}
		})
	}
}

func TestCompareUsesCustomEquality(t *testing.T) {
	base := []string{"a", "b"}
	local := []string{"b", "a"}
	remote := []string{"a", "b"}

	comparison := Compare(base, local, remote, func(left, right []string) bool {
		if len(left) != len(right) {
			return false
		}

		sortedLeft := append([]string(nil), left...)
		sortedRight := append([]string(nil), right...)
		sortStrings(sortedLeft)
		sortStrings(sortedRight)
		return reflect.DeepEqual(sortedLeft, sortedRight)
	})

	if comparison.Outcome != OutcomeNoChange {
		t.Fatalf("expected no-change with custom equality, got %s", comparison.Outcome)
	}
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
