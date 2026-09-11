package core

import (
	"fmt"
	"strings"
)

// Priority is named rather than p0..p3 because every team disagrees about which
// end of a numeric scale is hottest, and the ambiguity lands on the agent.
// Stored as an int so ORDER BY is cheap; p0..p3 are accepted as input aliases.
type Priority int

const (
	PriorityUrgent Priority = 0
	PriorityHigh   Priority = 1
	PriorityNormal Priority = 2
	PriorityLow    Priority = 3
)

var priorityNames = [...]string{"urgent", "high", "normal", "low"}

func (p Priority) String() string {
	if p < 0 || int(p) >= len(priorityNames) {
		return "normal"
	}
	return priorityNames[p]
}

func ParsePriority(s string) (Priority, error) {
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case "urgent", "p0":
		return PriorityUrgent, nil
	case "high", "p1":
		return PriorityHigh, nil
	case "normal", "p2":
		return PriorityNormal, nil
	case "low", "p3":
		return PriorityLow, nil
	default:
		return 0, ErrUsage("bad_priority",
			fmt.Sprintf("unknown priority %q", s),
			"use one of: urgent, high, normal, low")
	}
}
