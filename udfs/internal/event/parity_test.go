package event

import (
	"encoding/json"
	"testing"

	"numa-perfman/internal/oracle"
)

// Golden values captured for fixed source events. These tests lock enrichment,
// JSON shape, fan-out, and dedup keys to the contract that produces
// expected_sink_events.

func TestEnrichmentScalarsMatchDatagen(t *testing.T) {
	if got := DeterministicGeo("10.1.2.3"); got != "IN-MUM" {
		t.Errorf("geo(10.1.2.3) = %q, want IN-MUM", got)
	}
	if got := DeterministicGeo("8.8.8.8"); got != "DE-BER" {
		t.Errorf("geo(8.8.8.8) = %q, want DE-BER", got)
	}
	if got := DeterministicGeo("192.168.0.1"); got != "BR-SAO" {
		t.Errorf("geo(192.168.0.1) = %q, want BR-SAO", got)
	}

	if got := DeterministicCampaign("banner"); got != "campaign-8c7ed2d9" {
		t.Errorf("campaign(banner) = %q, want campaign-8c7ed2d9", got)
	}
	if got := DeterministicCampaign("native"); got != "campaign-bef32d2c" {
		t.Errorf("campaign(native) = %q, want campaign-bef32d2c", got)
	}
	if got := DeterministicCampaign("video"); got != "campaign-0cab1c96" {
		t.Errorf("campaign(video) = %q, want campaign-0cab1c96", got)
	}

	for userID, want := range map[string]float64{
		"user-42": 0.4279,
		"user-99": 0.5293,
		"user-7":  0.0357,
	} {
		if got := DeterministicRiskScore(userID); got != want {
			t.Errorf("risk(%s) = %v, want %v", userID, got, want)
		}
	}
}

func TestMapPayloadsMatchDatagen(t *testing.T) {
	events := map[string]Event{
		"evt-1": {EventID: "evt-1", UserID: "user-42", PageID: "page-7", AdType: "banner", EventType: "impression", EventTime: 1700000000000, IpAddress: "10.1.2.3"},
		"evt-2": {EventID: "evt-2", UserID: "user-99", PageID: "page-1", AdType: "video", EventType: "click", EventTime: 1700000000001, IpAddress: "8.8.8.8"},
		"evt-3": {EventID: "evt-3", UserID: "user-7", PageID: "page-3", AdType: "native", EventType: "conversion", EventTime: 1700000000002, IpAddress: "192.168.0.1"},
	}

	// dedup_key -> golden JSON payload produced by the datagen.
	golden := map[string]string{
		"evt-1_unary-map_0":  `{"event_id":"evt-1","user_id":"user-42","page_id":"page-7","ad_type":"banner","event_type":"impression","event_time":1700000000000,"ip_address":"10.1.2.3","geo_location":"IN-MUM","processed_by":"unary-map","child_index":"0","total_children":"2","transformer_child_index":0,"transformer_total_children":1}`,
		"evt-1_unary-map_1":  `{"event_id":"evt-1","user_id":"user-42","page_id":"page-7","ad_type":"banner","event_type":"impression_detail","event_time":1700000000000,"ip_address":"10.1.2.3","geo_location":"IN-MUM","processed_by":"unary-map","child_index":"1","total_children":"2","transformer_child_index":0,"transformer_total_children":1}`,
		"evt-1_batch-map_0":  `{"event_id":"evt-1","user_id":"user-42","page_id":"page-7","ad_type":"banner","event_type":"impression","event_time":1700000000000,"ip_address":"10.1.2.3","campaign_id":"campaign-8c7ed2d9","processed_by":"batch-map","child_index":"0","total_children":"1","transformer_child_index":0,"transformer_total_children":1}`,
		"evt-1_stream-map_0": `{"event_id":"evt-1","user_id":"user-42","page_id":"page-7","ad_type":"banner","event_type":"impression","event_time":1700000000000,"ip_address":"10.1.2.3","risk_score":0.4279,"processed_by":"stream-map","child_index":"0","total_children":"2","transformer_child_index":0,"transformer_total_children":1}`,
		"evt-1_stream-map_1": `{"event_id":"evt-1","user_id":"user-42","page_id":"page-7","ad_type":"banner","event_type":"impression_scored","event_time":1700000000000,"ip_address":"10.1.2.3","risk_score":0.4279,"processed_by":"stream-map","child_index":"1","total_children":"2","transformer_child_index":0,"transformer_total_children":1}`,
		"evt-2_batch-map_0":  `{"event_id":"evt-2","user_id":"user-99","page_id":"page-1","ad_type":"video","event_type":"click","event_time":1700000000001,"ip_address":"8.8.8.8","campaign_id":"campaign-0cab1c96","processed_by":"batch-map","child_index":"0","total_children":"3","transformer_child_index":0,"transformer_total_children":1}`,
		"evt-2_batch-map_1":  `{"event_id":"evt-2","user_id":"user-99","page_id":"page-1","ad_type":"video_analytics","event_type":"click","event_time":1700000000001,"ip_address":"8.8.8.8","campaign_id":"campaign-0cab1c96","processed_by":"batch-map","child_index":"1","total_children":"3","transformer_child_index":0,"transformer_total_children":1}`,
		"evt-2_batch-map_2":  `{"event_id":"evt-2","user_id":"user-99","page_id":"page-1","ad_type":"video_billing","event_type":"click","event_time":1700000000001,"ip_address":"8.8.8.8","campaign_id":"campaign-0cab1c96","processed_by":"batch-map","child_index":"2","total_children":"3","transformer_child_index":0,"transformer_total_children":1}`,
	}

	for dedupKey, want := range golden {
		ev, mapName, childIdx := splitDedup(t, dedupKey, events)
		outputs := ProduceOutputs(ev, mapName)
		var match *Event
		for i := range outputs {
			if outputs[i].ChildIndex == childIdx {
				match = &outputs[i]
				break
			}
		}
		if match == nil {
			t.Errorf("%s: no output with child_index %s", dedupKey, childIdx)
			continue
		}
		got, _ := json.Marshal(*match)
		if string(got) != want {
			t.Errorf("%s payload mismatch:\n got: %s\nwant: %s", dedupKey, got, want)
		}
		if gotKey := DedupKey(ev.EventID, mapName, match.ChildIndex); gotKey != dedupKey {
			t.Errorf("dedup key mismatch: got %s want %s", gotKey, dedupKey)
		}
	}
}

func TestFanoutCountsMatchDatagen(t *testing.T) {
	banner := Event{AdType: "banner", EventType: "impression"}
	click := Event{AdType: "native", EventType: "click"}
	other := Event{AdType: "native", EventType: "view"}

	if n := FanoutCount(banner, RouteTagUnaryMap); n != 2 {
		t.Errorf("unary banner fanout = %d, want 2", n)
	}
	if n := FanoutCount(other, RouteTagUnaryMap); n != 1 {
		t.Errorf("unary non-banner fanout = %d, want 1", n)
	}
	if n := FanoutCount(click, RouteTagBatchMap); n != 3 {
		t.Errorf("batch click fanout = %d, want 3", n)
	}
	if n := FanoutCount(other, RouteTagBatchMap); n != 1 {
		t.Errorf("batch non-click fanout = %d, want 1", n)
	}
	if n := FanoutCount(other, RouteTagStreamMap); n != 2 {
		t.Errorf("stream fanout = %d, want 2", n)
	}
}

func TestMapOutputsMatchIndependentOracle(t *testing.T) {
	source := Event{
		EventID: "evt-parity", UserID: "user-42", PageID: "page-7",
		AdType: "banner", EventType: "click", EventTime: 1700000000000,
		IpAddress: "10.1.2.3",
	}
	oracleSource := oracle.Event{
		EventID: source.EventID, UserID: source.UserID, PageID: source.PageID,
		AdType: source.AdType, EventType: source.EventType, EventTime: source.EventTime,
		IpAddress: source.IpAddress,
	}
	for _, route := range []string{RouteTagUnaryMap, RouteTagBatchMap, RouteTagStreamMap} {
		actual, err := json.Marshal(ProduceOutputs(source, route))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := json.Marshal(oracle.ProduceMapOutputs(oracleSource, route))
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != string(expected) {
			t.Fatalf("%s output drift:\nUDF: %s\noracle: %s", route, actual, expected)
		}
	}
}

func TestMonoVertexFanoutMatchDatagen(t *testing.T) {
	transformer := map[string]int{"banner": 0, "interstitial": 1, "native": 2, "video": 3}
	for adType, want := range transformer {
		if n := TransformerFanoutCount(Event{AdType: adType}); n != want {
			t.Errorf("transformer fanout %s = %d, want %d", adType, n, want)
		}
	}
	mapFanout := map[string]int{"view": 0, "impression": 1, "click": 2, "conversion": 3}
	for evType, want := range mapFanout {
		if n := MapFanoutCount(Event{EventType: evType}); n != want {
			t.Errorf("map fanout %s = %d, want %d", evType, n, want)
		}
	}
	if got := MonoVertexChildIndex(0, 0); got != "0-0" {
		t.Errorf("childIndex(0,0) = %q, want 0-0", got)
	}
	if got := MonoVertexTotalChildren(3, 2); got != "3-2" {
		t.Errorf("totalChildren(3,2) = %q, want 3-2", got)
	}
	if got := MonoVertexTotalChildren(2, 3); got != "2-3" {
		t.Errorf("totalChildren(2,3) = %q, want 2-3", got)
	}
}

func TestReduceOutputJSONIsDeterministic(t *testing.T) {
	out := ReduceOutput{
		ReduceKey:      "key-42",
		WindowStart:    1451606460000,
		WindowEnd:      1451606520000,
		EventCount:     3,
		TotalAmount:    600,
		MinAmount:      100,
		MaxAmount:      300,
		CategoryCounts: map[string]int{"gamma": 1, "alpha": 2},
	}
	// Field order matches the datagen's marshalDeterministic; map keys are sorted by
	// encoding/json (alpha before gamma).
	want := `{"reduce_key":"key-42","window_start":1451606460000,"window_end":1451606520000,"event_count":3,"total_amount":600,"category_counts":{"alpha":2,"gamma":1},"min_amount":100,"max_amount":300}`
	got, err := out.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != want {
		t.Errorf("reduce output mismatch:\n got: %s\nwant: %s", got, want)
	}
	if key := ReduceDedupKey("key-42", 1451606460000); key != "key-42_1451606460000" {
		t.Errorf("reduce dedup key = %q, want key-42_1451606460000", key)
	}
}

func splitDedup(t *testing.T, dedupKey string, events map[string]Event) (Event, string, string) {
	t.Helper()
	// dedup keys here are {evt-N}_{map-name}_{childIndex}; event IDs contain no extra
	// underscores, map names are unary-map/batch-map/stream-map.
	for id, ev := range events {
		for _, mapName := range []string{RouteTagUnaryMap, RouteTagBatchMap, RouteTagStreamMap} {
			prefix := id + "_" + mapName + "_"
			if len(dedupKey) > len(prefix) && dedupKey[:len(prefix)] == prefix {
				return ev, mapName, dedupKey[len(prefix):]
			}
		}
	}
	t.Fatalf("could not parse dedup key %q", dedupKey)
	return Event{}, "", ""
}
