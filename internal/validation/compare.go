package validation

import "numa-perfman/internal/oracle"

// PhysicalDelivery is one observed sink row (may include duplicate receive counts).
type PhysicalDelivery struct {
	LogicalKey    string
	EventID       string
	ProcessedBy   string
	ChildIndex    string
	TotalChildren string
	Payload       []byte
	ReceiveCount  int
}

// CompareOptions bounds failure sample collection.
type CompareOptions struct {
	MaxSamples int
}

// FailureSample describes one correctness defect.
type FailureSample struct {
	Kind       string `json:"kind"`
	LogicalKey string `json:"logical_key,omitempty"`
	Expected   string `json:"expected,omitempty"`
	Actual     string `json:"actual,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// CompareResult summarizes expected vs physical deliveries.
type CompareResult struct {
	Passed bool

	SourceCount            int64
	ExpectedLogicalCount   int64
	LogicalActualCount     int64
	PhysicalDeliveryCount  int64
	DuplicateDeliveryCount int64
	DuplicateRate          float64

	MissingCount    int64
	UnexpectedCount int64
	CorruptedCount  int64
	RoutingMismatch int64
	ChildMismatch   int64

	Samples []FailureSample
}

// CompareExpected matches oracle expected outputs to physical sink deliveries.
// Duplicate physical deliveries collapse by logical key; extra receive counts are reported but do not fail.
func CompareExpected(expected []oracle.ExpectedLogicalOutput, actual []PhysicalDelivery, opts CompareOptions) CompareResult {
	if opts.MaxSamples == 0 {
		opts.MaxSamples = 20
	}

	expByKey := make(map[string]oracle.ExpectedLogicalOutput, len(expected))
	for _, e := range expected {
		expByKey[e.LogicalKey] = e
	}

	collapsed := collapsePhysical(actual)
	res := CompareResult{
		ExpectedLogicalCount:  int64(len(expected)),
		LogicalActualCount:    int64(len(collapsed)),
		PhysicalDeliveryCount: int64(len(actual)),
	}

	var dup int64
	for _, d := range collapsed {
		if d.ReceiveCount > 1 {
			dup += int64(d.ReceiveCount - 1)
		}
	}
	res.DuplicateDeliveryCount = dup
	if res.PhysicalDeliveryCount > 0 {
		res.DuplicateRate = float64(dup) / float64(res.PhysicalDeliveryCount)
	}

	seenActual := make(map[string]struct{}, len(collapsed))
	for key, d := range collapsed {
		seenActual[key] = struct{}{}
		exp, ok := expByKey[key]
		if !ok {
			res.UnexpectedCount++
			appendSample(&res, opts.MaxSamples, FailureSample{Kind: "unexpected", LogicalKey: key})
			continue
		}
		if d.ProcessedBy != exp.ProcessedBy {
			res.RoutingMismatch++
			appendSample(&res, opts.MaxSamples, FailureSample{
				Kind: "routing", LogicalKey: key,
				Expected: exp.ProcessedBy, Actual: d.ProcessedBy,
			})
		}
		if d.ChildIndex != exp.ChildIndex || d.TotalChildren != exp.TotalChildren {
			res.ChildMismatch++
			appendSample(&res, opts.MaxSamples, FailureSample{
				Kind: "child", LogicalKey: key,
				Detail: fmtChild(exp.ChildIndex, exp.TotalChildren, d.ChildIndex, d.TotalChildren),
			})
		}
		if !oracle.PayloadsEqual(exp.Payload, d.Payload) {
			res.CorruptedCount++
			appendSample(&res, opts.MaxSamples, FailureSample{
				Kind: "corrupted", LogicalKey: key,
				Expected: string(exp.Payload), Actual: string(d.Payload),
			})
		}
	}

	for _, exp := range expected {
		key := exp.LogicalKey
		if _, ok := seenActual[key]; !ok {
			res.MissingCount++
			appendSample(&res, opts.MaxSamples, FailureSample{
				Kind:       "missing",
				LogicalKey: key,
				Expected:   expectedOutputPath(exp),
				Detail:     missingOutputDetail(exp),
			})
		}
	}

	res.Passed = res.MissingCount == 0 && res.UnexpectedCount == 0 &&
		res.CorruptedCount == 0 && res.RoutingMismatch == 0 && res.ChildMismatch == 0
	return res
}

func collapsePhysical(rows []PhysicalDelivery) map[string]PhysicalDelivery {
	out := make(map[string]PhysicalDelivery, len(rows))
	for _, row := range rows {
		cur, ok := out[row.LogicalKey]
		if !ok {
			if row.ReceiveCount == 0 {
				row.ReceiveCount = 1
			}
			out[row.LogicalKey] = row
			continue
		}
		cur.ReceiveCount += physicalReceiveCount(row)
		out[row.LogicalKey] = cur
	}
	return out
}

func physicalReceiveCount(row PhysicalDelivery) int {
	if row.ReceiveCount > 0 {
		return row.ReceiveCount
	}
	return 1
}

func appendSample(res *CompareResult, max int, s FailureSample) {
	if len(res.Samples) >= max {
		return
	}
	res.Samples = append(res.Samples, s)
}

func fmtChild(expIdx, expTot, actIdx, actTot string) string {
	return "expected child " + expIdx + "/" + expTot + " actual " + actIdx + "/" + actTot
}

func expectedOutputPath(exp oracle.ExpectedLogicalOutput) string {
	if exp.ProcessedBy != "" {
		return exp.ProcessedBy
	}
	return "unknown"
}

func missingOutputDetail(exp oracle.ExpectedLogicalOutput) string {
	detail := "path=" + expectedOutputPath(exp)
	if exp.EventID != "" {
		detail += " event_id=" + exp.EventID
	}
	if exp.ChildIndex != "" || exp.TotalChildren != "" {
		detail += " child=" + exp.ChildIndex + "/" + exp.TotalChildren
	}
	return detail
}
