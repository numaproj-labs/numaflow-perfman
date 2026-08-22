// Package event holds the shared event model and deterministic enrichment logic
// for the validation UDFs.
//
// This is a faithful Go port of the Kotlin numaflow-data-validations event package.
// CRITICAL: the Event struct, enrichment functions, fan-out rules and dedup-key
// format MUST stay byte/value-identical to the independent generator in
// numa-perfman/internal/oracle. The oracle pre-computes expected_sink_events;
// these UDFs produce sink_events. Validation compares canonical payloads, so any
// divergence in JSON shape, hashing, routing, or rounding causes a failure.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
)

// Route tags for conditional forwarding (map-validations pipeline).
const (
	RouteTagUnaryMap  = "unary-map"
	RouteTagBatchMap  = "batch-map"
	RouteTagStreamMap = "stream-map"
)

// Ad types and event types.
const (
	AdTypeBanner = "banner"
	EventClick   = "click"
)

// GeoLocations is the deterministic geo lookup table. Order and contents must
// match oracle.GeoLocations exactly.
var GeoLocations = []string{"US-NY", "US-CA", "UK-LDN", "DE-BER", "JP-TKY", "AU-SYD", "IN-MUM", "BR-SAO"}

// Event represents a source event and its enriched form.
// JSON tags use snake_case to match the Kotlin @SerialName annotations and the
// oracle.Event. Pointer fields with omitzero are omitted
// from JSON when unset, exactly mirroring the expected_sink_events payloads.
type Event struct {
	EventID       string   `json:"event_id"`
	UserID        string   `json:"user_id"`
	PageID        string   `json:"page_id"`
	AdType        string   `json:"ad_type"`
	EventType     string   `json:"event_type"`
	EventTime     int64    `json:"event_time"`
	IpAddress     string   `json:"ip_address"`
	RouteTag      *string  `json:"route_tag,omitzero"`
	GeoLocation   *string  `json:"geo_location,omitzero"`
	CampaignID    *string  `json:"campaign_id,omitzero"`
	RiskScore     *float64 `json:"risk_score,omitzero"`
	ProcessedBy   *string  `json:"processed_by,omitzero"`
	ChildIndex    string   `json:"child_index"`
	TotalChildren string   `json:"total_children"`
	// Stamped by the MonoVertex transformer so downstream stages can compose the
	// final composite child_index. Harmless for non-MonoVertex flows.
	TransformerChildIndex    int `json:"transformer_child_index"`
	TransformerTotalChildren int `json:"transformer_total_children"`
}

// FanoutCount returns the number of output messages for a given event and map name.
// Mirrors oracle map fan-out and Kotlin Fanout.fanoutCount.
func FanoutCount(e Event, mapName string) int {
	switch mapName {
	case RouteTagUnaryMap:
		if e.AdType == AdTypeBanner {
			return 2
		}
		return 1
	case RouteTagBatchMap:
		if e.EventType == EventClick {
			return 3
		}
		return 1
	case RouteTagStreamMap:
		return 2
	default:
		return 1
	}
}

// ProduceOutputs generates the enriched output events for a source event routed to
// the given map vertex. This must exactly match oracle.ProduceMapOutputs.
func ProduceOutputs(in Event, mapName string) []Event {
	count := FanoutCount(in, mapName)
	outputs := make([]Event, count)

	for i := range count {
		out := Event{
			EventID:                  in.EventID,
			UserID:                   in.UserID,
			PageID:                   in.PageID,
			AdType:                   in.AdType,
			EventType:                in.EventType,
			EventTime:                in.EventTime,
			IpAddress:                in.IpAddress,
			ChildIndex:               strconv.Itoa(i),
			TotalChildren:            strconv.Itoa(count),
			TransformerTotalChildren: 1,
		}
		pb := mapName
		out.ProcessedBy = &pb

		switch mapName {
		case RouteTagUnaryMap:
			geo := DeterministicGeo(in.IpAddress)
			out.GeoLocation = &geo
			if i == 1 {
				out.EventType = in.EventType + "_detail"
			}

		case RouteTagBatchMap:
			campaign := DeterministicCampaign(in.AdType)
			out.CampaignID = &campaign
			switch i {
			case 1:
				out.AdType = in.AdType + "_analytics"
			case 2:
				out.AdType = in.AdType + "_billing"
			}

		case RouteTagStreamMap:
			risk := DeterministicRiskScore(in.UserID)
			out.RiskScore = &risk
			if i == 1 {
				out.EventType = in.EventType + "_scored"
			}
		}

		outputs[i] = out
	}
	return outputs
}

// DedupKey generates the composite dedup key for a sink event.
// Format: {event_id}_{processed_by}_{child_index}. Mirrors oracle.DedupKey.
func DedupKey(eventID, processedBy, childIndex string) string {
	return eventID + "_" + processedBy + "_" + childIndex
}

// DeterministicGeo computes a geo location from an IP address using SHA256.
// Uses int(hash[0]) % len(GeoLocations) to match the independent oracle.
func DeterministicGeo(ipAddress string) string {
	hash := sha256.Sum256([]byte(ipAddress))
	idx := int(hash[0]) % len(GeoLocations)
	return GeoLocations[idx]
}

// DeterministicCampaign computes a campaign ID from an ad type using SHA256.
func DeterministicCampaign(adType string) string {
	hash := sha256.Sum256([]byte(adType))
	return "campaign-" + hex.EncodeToString(hash[:4])
}

// DeterministicRiskScore computes a risk score from a user ID using SHA256.
func DeterministicRiskScore(userID string) float64 {
	hash := sha256.Sum256([]byte(userID))
	raw := uint32(hash[0])<<24 | uint32(hash[1])<<16 | uint32(hash[2])<<8 | uint32(hash[3])
	value := float64(raw) / float64(math.MaxUint32)
	return math.Round(value*10000) / 10000
}
