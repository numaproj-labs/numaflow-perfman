package oracle

import "testing"

func TestEnrichmentScalars(t *testing.T) {
	t.Parallel()
	ev := Event{IpAddress: "10.1.2.3", AdType: "interstitial", EventType: "impression", UserID: "user-42"}
	out := ProduceMapOutputs(ev, RouteUnaryMap)
	if out[0].GeoLocation == nil || *out[0].GeoLocation != "IN-MUM" {
		t.Fatalf("geo mismatch")
	}
	ev2 := Event{AdType: "banner"}
	out2 := ProduceMapOutputs(ev2, RouteBatchMap)
	if out2[0].CampaignID == nil || *out2[0].CampaignID != "campaign-8c7ed2d9" {
		t.Fatalf("campaign mismatch")
	}
}
