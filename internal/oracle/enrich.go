package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
)

func fanoutCount(e Event, mapName string) int {
	switch mapName {
	case RouteUnaryMap:
		if e.AdType == "banner" {
			return 2
		}
		return 1
	case RouteBatchMap:
		if e.EventType == "click" {
			return 3
		}
		return 1
	case RouteStreamMap:
		return 2
	default:
		return 1
	}
}

// ProduceMapOutputs returns expected sink events for a source event routed to mapName.
func ProduceMapOutputs(in Event, mapName string) []Event {
	count := fanoutCount(in, mapName)
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
		case RouteUnaryMap:
			geo := deterministicGeo(in.IpAddress)
			out.GeoLocation = &geo
			if i == 1 {
				out.EventType = in.EventType + "_detail"
			}
		case RouteBatchMap:
			campaign := deterministicCampaign(in.AdType)
			out.CampaignID = &campaign
			switch i {
			case 1:
				out.AdType = in.AdType + "_analytics"
			case 2:
				out.AdType = in.AdType + "_billing"
			}
		case RouteStreamMap:
			risk := deterministicRiskScore(in.UserID)
			out.RiskScore = &risk
			if i == 1 {
				out.EventType = in.EventType + "_scored"
			}
		}
		outputs[i] = out
	}
	return outputs
}

func deterministicGeo(ipAddress string) string {
	hash := sha256.Sum256([]byte(ipAddress))
	idx := int(hash[0]) % len(GeoLocations)
	return GeoLocations[idx]
}

func deterministicCampaign(adType string) string {
	hash := sha256.Sum256([]byte(adType))
	return "campaign-" + hex.EncodeToString(hash[:4])
}

func deterministicRiskScore(userID string) float64 {
	hash := sha256.Sum256([]byte(userID))
	raw := uint32(hash[0])<<24 | uint32(hash[1])<<16 | uint32(hash[2])<<8 | uint32(hash[3])
	value := float64(raw) / float64(math.MaxUint32)
	return math.Round(value*10000) / 10000
}

func mapDedupKey(eventID, processedBy, childIndex string) string {
	return eventID + "_" + processedBy + "_" + childIndex
}

func reduceDedupKey(reduceKey string, windowStart int64) string {
	return reduceKey + "_" + strconv.FormatInt(windowStart, 10)
}

func transformerFanoutCount(e Event) int {
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

func mapFanoutCount(e Event) int {
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

func monoVertexChildIndex(t, m int) string {
	return strconv.Itoa(t) + "-" + strconv.Itoa(m)
}

func monoVertexTotalChildren(t, m int) string {
	return strconv.Itoa(t) + "-" + strconv.Itoa(m)
}

func monoVertexEnrichEvent(event Event) Event {
	geo := deterministicGeo(event.IpAddress)
	event.GeoLocation = &geo
	return event
}
