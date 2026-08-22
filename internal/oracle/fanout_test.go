package oracle_test

import (
	"testing"
	"testing/quick"

	"numa-perfman/internal/oracle"
)

func TestFanoutCardinalityProperty(t *testing.T) {
	t.Parallel()
	f := func(adIdx, evIdx uint8) bool {
		e := oracle.Event{
			EventID: "evt-x", AdType: oracle.AdTypes[int(adIdx)%len(oracle.AdTypes)],
			EventType: oracle.EventTypes[int(evIdx)%len(oracle.EventTypes)],
			UserID:    "user-1", IpAddress: "1.2.3.4",
		}
		for _, route := range oracle.MapRouteTags {
			n := oracle.FanoutCardinality(e, route)
			outs := oracle.ProduceMapOutputs(e, route)
			if n != len(outs) {
				return false
			}
			switch route {
			case oracle.RouteUnaryMap:
				if e.AdType == "banner" && n != 2 {
					return false
				}
				if e.AdType != "banner" && n != 1 {
					return false
				}
			case oracle.RouteBatchMap:
				if e.EventType == "click" && n != 3 {
					return false
				}
				if e.EventType != "click" && n != 1 {
					return false
				}
			case oracle.RouteStreamMap:
				if n != 2 {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(f, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMonoVertexFanoutNonNegative(t *testing.T) {
	t.Parallel()
	for _, ad := range oracle.AdTypes {
		for _, ev := range oracle.EventTypes {
			e := oracle.Event{EventID: "e1", AdType: ad, EventType: ev, UserID: "u", IpAddress: "9.9.9.9"}
			for _, route := range oracle.MonoVertexRouteTags {
				if oracle.FanoutCardinality(e, route) < 0 {
					t.Fatalf("negative cardinality")
				}
			}
		}
	}
}
