package event

import "strconv"

// Two-stage MonoVertex fan-out rules. Must mirror pipelines.TransformerFanoutCount /
// MapFanoutCount / MonoVertexChildIndex / MonoVertexTotalChildren in the orchestrator's
// datagen, and the Kotlin MonoVertexFanout object.

// TransformerFanoutCount returns the transformer fan-out count by ad_type.
// 0 = drop the entire source event.
func TransformerFanoutCount(e Event) int {
	switch e.AdType {
	case "banner":
		return 0
	case "interstitial":
		return 1
	case "native":
		return 2
	case "video":
		return 3
	default:
		return 1
	}
}

// MapFanoutCount returns the map fan-out count by event_type.
// 0 = drop this transformer child. Only applies to normal-* flows.
func MapFanoutCount(e Event) int {
	switch e.EventType {
	case "view":
		return 0
	case "impression":
		return 1
	case "click":
		return 2
	case "conversion":
		return 3
	default:
		return 1
	}
}

// MonoVertexChildIndex composes the two-stage child index string "t-m".
func MonoVertexChildIndex(t, m int) string {
	return strconv.Itoa(t) + "-" + strconv.Itoa(m)
}

// MonoVertexTotalChildren composes the two-stage total children string "T-M".
func MonoVertexTotalChildren(T, M int) string {
	return strconv.Itoa(T) + "-" + strconv.Itoa(M)
}
