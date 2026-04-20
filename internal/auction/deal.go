package auction

// Priority returns the priority score for a given DealType.
// Higher score means higher priority.
func Priority(dt DealType) int {
	switch dt {
	case PG:
		return 3
	case PMP:
		return 2
	case Open:
		return 1
	default:
		return 0
	}
}
