package oracle

// Route tags for map and monovertex conditional forwarding.
const (
	RouteUnaryMap  = "unary-map"
	RouteBatchMap  = "batch-map"
	RouteStreamMap = "stream-map"
)

var MapRouteTags = []string{RouteUnaryMap, RouteBatchMap, RouteStreamMap}

var MonoVertexRouteTags = []string{
	"normal-onsuccess",
	"normal-fallback",
	"bypass-onsuccess",
	"bypass-fallback",
}

var (
	AdTypes      = []string{"banner", "interstitial", "native", "video"}
	EventTypes   = []string{"click", "impression", "conversion", "view"}
	GeoLocations = []string{
		"US-NY", "US-CA", "UK-LDN", "DE-BER", "JP-TKY", "AU-SYD", "IN-MUM", "BR-SAO",
	}
)

// Event is the shared map / monovertex event model (JSON field names are snake_case).
type Event struct {
	EventID                  string   `json:"event_id"`
	UserID                   string   `json:"user_id"`
	PageID                   string   `json:"page_id"`
	AdType                   string   `json:"ad_type"`
	EventType                string   `json:"event_type"`
	EventTime                int64    `json:"event_time"`
	IpAddress                string   `json:"ip_address"`
	RouteTag                 *string  `json:"route_tag,omitzero"`
	GeoLocation              *string  `json:"geo_location,omitzero"`
	CampaignID               *string  `json:"campaign_id,omitzero"`
	RiskScore                *float64 `json:"risk_score,omitzero"`
	ProcessedBy              *string  `json:"processed_by,omitzero"`
	ChildIndex               string   `json:"child_index"`
	TotalChildren            string   `json:"total_children"`
	TransformerChildIndex    int      `json:"transformer_child_index"`
	TransformerTotalChildren int      `json:"transformer_total_children"`
}

// MapSourceEvent is a source row for the map scenario.
type MapSourceEvent struct {
	Event
	RouteTag string
}

// ReduceSourceEvent is a reduce pipeline source event (EventTime is message event time, not payload).
type ReduceSourceEvent struct {
	EventID   string `json:"event_id"`
	ReduceKey string `json:"reduce_key"`
	Category  string `json:"category"`
	Amount    int    `json:"amount"`
	EventTime int64  `json:"-"`
}

// ReduceOutput is the canonical reduce / sliding-reduce sink payload.
type ReduceOutput struct {
	ReduceKey      string         `json:"reduce_key"`
	WindowStart    int64          `json:"window_start"`
	WindowEnd      int64          `json:"window_end"`
	EventCount     int            `json:"event_count"`
	TotalAmount    int64          `json:"total_amount"`
	CategoryCounts map[string]int `json:"category_counts"`
	MinAmount      int            `json:"min_amount"`
	MaxAmount      int            `json:"max_amount"`
}

const ReduceKeyCount = 100

var ReduceCategories = []string{"alpha", "beta", "gamma", "delta"}
