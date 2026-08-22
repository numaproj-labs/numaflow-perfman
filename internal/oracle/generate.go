package oracle

import (
	"fmt"
	"math/rand/v2"
	"sort"
)

// ExpectedLogicalOutput is one expected sink delivery (logical key + metadata + payload).
type ExpectedLogicalOutput struct {
	LogicalKey    string
	EventID       string
	ProcessedBy   string
	ChildIndex    string
	TotalChildren string
	Payload       []byte
}

// Bundle holds generated source inputs and expected logical outputs for a scenario run.
type Bundle struct {
	Scenario Scenario
	Config   GenerationConfig

	MapSources    []MapSourceEvent
	ReduceSources []ReduceSourceEvent
	Expected      []ExpectedLogicalOutput
}

// Generate builds deterministic source events and expected logical outputs for scenario.
func Generate(scenario Scenario, cfg GenerationConfig) (Bundle, error) {
	cfg = cfg.withDefaults()
	if !scenario.Valid() {
		return Bundle{}, fmt.Errorf("unknown scenario %q", scenario)
	}
	if cfg.EventCount < 0 {
		return Bundle{}, fmt.Errorf("event count must be non-negative")
	}

	switch scenario {
	case ScenarioMap:
		return generateMap(cfg), nil
	case ScenarioReduce:
		return generateFixedReduce(cfg), nil
	case ScenarioSlidingReduce:
		return generateSlidingReduce(cfg), nil
	case ScenarioMonoVertex:
		return generateMonoVertex(cfg), nil
	default:
		return Bundle{}, fmt.Errorf("unsupported scenario %q", scenario)
	}
}

func generateMap(cfg GenerationConfig) Bundle {
	rng := newRNG(cfg.Seed)
	b := Bundle{Scenario: ScenarioMap, Config: cfg}
	for i := int64(0); i < cfg.EventCount; i++ {
		route := MapRouteTags[i%int64(len(MapRouteTags))]
		ev := randomMapEvent(rng, cfg, i)
		b.MapSources = append(b.MapSources, MapSourceEvent{Event: ev, RouteTag: route})
		for _, out := range ProduceMapOutputs(ev, route) {
			payload, _ := CanonicalMapEventJSON(out)
			b.Expected = append(b.Expected, ExpectedLogicalOutput{
				LogicalKey:    mapDedupKey(ev.EventID, route, out.ChildIndex),
				EventID:       ev.EventID,
				ProcessedBy:   route,
				ChildIndex:    out.ChildIndex,
				TotalChildren: out.TotalChildren,
				Payload:       payload,
			})
		}
	}
	sortExpected(&b)
	return b
}

func generateFixedReduce(cfg GenerationConfig) Bundle {
	rng := newRNG(cfg.Seed)
	b := Bundle{Scenario: ScenarioReduce, Config: cfg}
	windows := make(map[windowKey]*windowAccumulator)
	for i := int64(0); i < cfg.EventCount; i++ {
		ev := randomReduceEvent(rng, cfg, i)
		b.ReduceSources = append(b.ReduceSources, ev)
		ws := (ev.EventTime / FixedWindowSizeMs) * FixedWindowSizeMs
		accumulateWindow(windows, windowKey{ReduceKey: ev.ReduceKey, WindowStart: ws}, ev)
	}
	appendReduceExpected(&b, windows, FixedWindowSizeMs, "fixed-window-reduce")
	return b
}

func generateSlidingReduce(cfg GenerationConfig) Bundle {
	rng := newRNG(cfg.Seed)
	b := Bundle{Scenario: ScenarioSlidingReduce, Config: cfg}
	windows := make(map[windowKey]*windowAccumulator)
	slideCfg := SlidingWindowConfig{LengthMs: SlidingWindowLengthMs, SlideMs: SlidingWindowSlideMs}
	for i := int64(0); i < cfg.EventCount; i++ {
		ev := randomReduceEvent(rng, cfg, i)
		b.ReduceSources = append(b.ReduceSources, ev)
		for _, ws := range AssignSlidingWindows(ev.EventTime, slideCfg) {
			accumulateWindow(windows, windowKey{ReduceKey: ev.ReduceKey, WindowStart: ws}, ev)
		}
	}
	appendReduceExpected(&b, windows, SlidingWindowLengthMs, "sliding-window-reduce")
	return b
}

type windowKey struct {
	ReduceKey   string
	WindowStart int64
}

type windowAccumulator struct {
	EventCount     int
	TotalAmount    int64
	CategoryCounts map[string]int
	MinAmount      int
	MaxAmount      int
	FirstEventID   string
}

func accumulateWindow(windows map[windowKey]*windowAccumulator, wk windowKey, ev ReduceSourceEvent) {
	acc, ok := windows[wk]
	if !ok {
		acc = &windowAccumulator{
			CategoryCounts: make(map[string]int),
			MinAmount:      ev.Amount,
			MaxAmount:      ev.Amount,
			FirstEventID:   ev.EventID,
		}
		windows[wk] = acc
	}
	acc.EventCount++
	acc.TotalAmount += int64(ev.Amount)
	acc.CategoryCounts[ev.Category]++
	if ev.Amount < acc.MinAmount {
		acc.MinAmount = ev.Amount
	}
	if ev.Amount > acc.MaxAmount {
		acc.MaxAmount = ev.Amount
	}
}

func appendReduceExpected(b *Bundle, windows map[windowKey]*windowAccumulator, windowLength int64, processedBy string) {
	for wk, acc := range windows {
		out := ReduceOutput{
			ReduceKey:      wk.ReduceKey,
			WindowStart:    wk.WindowStart,
			WindowEnd:      wk.WindowStart + windowLength,
			EventCount:     acc.EventCount,
			TotalAmount:    acc.TotalAmount,
			CategoryCounts: acc.CategoryCounts,
			MinAmount:      acc.MinAmount,
			MaxAmount:      acc.MaxAmount,
		}
		payload, _ := CanonicalReduceJSON(out)
		b.Expected = append(b.Expected, ExpectedLogicalOutput{
			LogicalKey:    reduceDedupKey(wk.ReduceKey, wk.WindowStart),
			EventID:       acc.FirstEventID,
			ProcessedBy:   processedBy,
			ChildIndex:    "0",
			TotalChildren: "1",
			Payload:       payload,
		})
	}
	sortExpected(b)
}

func generateMonoVertex(cfg GenerationConfig) Bundle {
	rng := newRNG(cfg.Seed)
	b := Bundle{Scenario: ScenarioMonoVertex, Config: cfg}
	for i := int64(0); i < cfg.EventCount; i++ {
		route := MonoVertexRouteTags[i%int64(len(MonoVertexRouteTags))]
		ev := randomMapEvent(rng, cfg, i)
		ev.RouteTag = &route
		b.MapSources = append(b.MapSources, MapSourceEvent{Event: ev, RouteTag: route})
		for _, exp := range produceMonoVertexExpected(ev, route) {
			payload, _ := CanonicalMapEventJSON(exp.event)
			b.Expected = append(b.Expected, ExpectedLogicalOutput{
				LogicalKey:    mapDedupKey(ev.EventID, exp.processedBy, exp.event.ChildIndex),
				EventID:       ev.EventID,
				ProcessedBy:   exp.processedBy,
				ChildIndex:    exp.event.ChildIndex,
				TotalChildren: exp.event.TotalChildren,
				Payload:       payload,
			})
		}
	}
	sortExpected(&b)
	return b
}

type monoExpected struct {
	event       Event
	processedBy string
}

func produceMonoVertexExpected(source Event, routeTag string) []monoExpected {
	t := transformerFanoutCount(source)
	if t == 0 {
		return nil
	}
	var out []monoExpected
	for ti := 0; ti < t; ti++ {
		switch routeTag {
		case "bypass-onsuccess", "bypass-fallback":
			child := source
			child.ChildIndex = monoVertexChildIndex(ti, 0)
			child.TotalChildren = monoVertexTotalChildren(t, 1)
			child.TransformerChildIndex = ti
			child.TransformerTotalChildren = t
			pb := "mv-bypass-onsuccess"
			if routeTag == "bypass-fallback" {
				pb = "mv-bypass-fallback"
			}
			child.ProcessedBy = &pb
			out = append(out, monoExpected{event: child, processedBy: pb})

		case "normal-onsuccess", "normal-fallback":
			m := mapFanoutCount(source)
			if m == 0 {
				continue
			}
			enriched := monoVertexEnrichEvent(source)
			enriched.TransformerChildIndex = ti
			enriched.TransformerTotalChildren = t
			for mi := 0; mi < m; mi++ {
				child := enriched
				child.ChildIndex = monoVertexChildIndex(ti, mi)
				child.TotalChildren = monoVertexTotalChildren(t, m)
				if routeTag == "normal-onsuccess" {
					primary := child
					pp := "mv-primary"
					primary.ProcessedBy = &pp
					out = append(out, monoExpected{event: primary, processedBy: pp})

					onSucc := child
					po := "mv-onsuccess"
					onSucc.ProcessedBy = &po
					out = append(out, monoExpected{event: onSucc, processedBy: po})
				} else {
					fb := child
					pf := "mv-fallback"
					fb.ProcessedBy = &pf
					out = append(out, monoExpected{event: fb, processedBy: pf})
				}
			}
		}
	}
	return out
}

func randomMapEvent(rng *rand.Rand, cfg GenerationConfig, index int64) Event {
	return Event{
		EventID:   fmt.Sprintf("evt-%s", randomUUID(rng)),
		UserID:    fmt.Sprintf("user-%d", rng.IntN(10000)),
		PageID:    fmt.Sprintf("page-%d", rng.IntN(500)),
		AdType:    AdTypes[rng.IntN(len(AdTypes))],
		EventType: EventTypes[rng.IntN(len(EventTypes))],
		EventTime: cfg.BaseEventTimeMs + index*cfg.SpacingMs,
		IpAddress: fmt.Sprintf("%d.%d.%d.%d", rng.IntN(256), rng.IntN(256), rng.IntN(256), rng.IntN(256)),
	}
}

func randomReduceEvent(rng *rand.Rand, cfg GenerationConfig, index int64) ReduceSourceEvent {
	return ReduceSourceEvent{
		EventID:   fmt.Sprintf("evt-%s", randomUUID(rng)),
		ReduceKey: fmt.Sprintf("key-%d", index%ReduceKeyCount),
		Category:  ReduceCategories[rng.IntN(len(ReduceCategories))],
		Amount:    rng.IntN(1000) + 1,
		EventTime: cfg.BaseEventTimeMs + index*cfg.SpacingMs,
	}
}

func sortExpected(b *Bundle) {
	sort.Slice(b.Expected, func(i, j int) bool {
		return b.Expected[i].LogicalKey < b.Expected[j].LogicalKey
	})
}

// FanoutCardinality returns the number of logical outputs for one map source event.
func FanoutCardinality(e Event, routeTag string) int {
	if routeTag == RouteUnaryMap || routeTag == RouteBatchMap || routeTag == RouteStreamMap {
		return len(ProduceMapOutputs(e, routeTag))
	}
	return len(produceMonoVertexExpected(e, routeTag))
}
