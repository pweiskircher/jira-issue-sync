package jira

import (
	"sort"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

func resolveTransitionSelection(transitions []Transition, selection contracts.TransitionSelection) TransitionResolution {
	sortedTransitions := sortedTransitionCopy(transitions)
	selectionKind := selection.Kind
	if selectionKind == "" {
		selectionKind = contracts.TransitionSelectionDynamic
	}

	switch selectionKind {
	case contracts.TransitionSelectionByID:
		candidate := strings.TrimSpace(selection.TransitionID)
		matches := matchTransitionsByID(sortedTransitions, candidate)
		return buildTransitionResolution(selectionKind, candidate, []string{candidate}, matches)
	case contracts.TransitionSelectionByName:
		candidate := strings.TrimSpace(selection.TransitionName)
		matches := matchTransitionsByName(sortedTransitions, candidate)
		return buildTransitionResolution(selectionKind, candidate, []string{candidate}, matches)
	case contracts.TransitionSelectionDynamic:
		candidates := normalizeCandidates(selection.DynamicStatusCandidates)
		tried := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			tried = append(tried, candidate)
			matches := matchTransitionsByTargetStatus(sortedTransitions, candidate)
			if len(matches) == 0 {
				continue
			}
			return buildTransitionResolution(selectionKind, candidate, tried, matches)
		}

		return TransitionResolution{
			Kind:            TransitionResolutionUnavailable,
			SelectionKind:   selectionKind,
			TriedCandidates: tried,
			ReasonCode:      contracts.ReasonCodeTransitionUnavailable,
		}
	default:
		return TransitionResolution{
			Kind:            TransitionResolutionUnavailable,
			SelectionKind:   selectionKind,
			ReasonCode:      contracts.ReasonCodeValidationFailed,
			TriedCandidates: nil,
		}
	}
}

func buildTransitionResolution(selectionKind contracts.TransitionSelectionKind, matchedCandidate string, triedCandidates []string, matches []Transition) TransitionResolution {
	sortedMatches := sortedTransitionCopy(matches)
	if len(sortedMatches) == 1 {
		return TransitionResolution{
			Kind:             TransitionResolutionSelected,
			SelectionKind:    selectionKind,
			MatchedCandidate: matchedCandidate,
			Transition:       sortedMatches[0],
			TriedCandidates:  append([]string(nil), triedCandidates...),
		}
	}

	if len(sortedMatches) > 1 {
		return TransitionResolution{
			Kind:             TransitionResolutionAmbiguous,
			SelectionKind:    selectionKind,
			MatchedCandidate: matchedCandidate,
			Matches:          sortedMatches,
			TriedCandidates:  append([]string(nil), triedCandidates...),
			ReasonCode:       contracts.ReasonCodeTransitionAmbiguous,
		}
	}

	return TransitionResolution{
		Kind:             TransitionResolutionUnavailable,
		SelectionKind:    selectionKind,
		MatchedCandidate: matchedCandidate,
		TriedCandidates:  append([]string(nil), triedCandidates...),
		ReasonCode:       contracts.ReasonCodeTransitionUnavailable,
	}
}

func sortedTransitionCopy(transitions []Transition) []Transition {
	if len(transitions) == 0 {
		return nil
	}

	copyTransitions := append([]Transition(nil), transitions...)
	sort.Slice(copyTransitions, func(i int, j int) bool {
		left := copyTransitions[i]
		right := copyTransitions[j]

		leftToStatus := strings.ToLower(strings.TrimSpace(left.ToStatusName))
		rightToStatus := strings.ToLower(strings.TrimSpace(right.ToStatusName))
		if leftToStatus != rightToStatus {
			return leftToStatus < rightToStatus
		}

		leftName := strings.ToLower(strings.TrimSpace(left.Name))
		rightName := strings.ToLower(strings.TrimSpace(right.Name))
		if leftName != rightName {
			return leftName < rightName
		}

		return strings.TrimSpace(left.ID) < strings.TrimSpace(right.ID)
	})
	return copyTransitions
}

func matchTransitionsByID(transitions []Transition, candidate string) []Transition {
	if candidate == "" {
		return nil
	}

	matches := make([]Transition, 0)
	for _, transition := range transitions {
		if strings.TrimSpace(transition.ID) == candidate {
			matches = append(matches, transition)
		}
	}
	return matches
}

func matchTransitionsByName(transitions []Transition, candidate string) []Transition {
	if candidate == "" {
		return nil
	}

	matches := make([]Transition, 0)
	for _, transition := range transitions {
		if strings.EqualFold(strings.TrimSpace(transition.Name), candidate) {
			matches = append(matches, transition)
		}
	}
	return matches
}

func matchTransitionsByTargetStatus(transitions []Transition, candidate string) []Transition {
	if candidate == "" {
		return nil
	}

	matches := make([]Transition, 0)
	for _, transition := range transitions {
		if strings.EqualFold(strings.TrimSpace(transition.ToStatusName), candidate) {
			matches = append(matches, transition)
		}
	}
	return matches
}

func normalizeCandidates(candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(candidates))
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}

		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	return normalized
}
