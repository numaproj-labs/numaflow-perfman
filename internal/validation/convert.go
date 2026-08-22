package validation

import "numa-perfman/internal/results"

// ValidationResultFromCompare maps in-memory compare output to persisted results.ValidationResult.
func ValidationResultFromCompare(runID string, cmp CompareResult, sourceCount int64) results.ValidationResult {
	return results.ValidationResult{
		RunID:                  runID,
		Passed:                 cmp.Passed,
		SourceCount:            sourceCount,
		ExpectedCount:          cmp.ExpectedLogicalCount,
		LogicalOutputCount:     cmp.LogicalActualCount,
		PhysicalDeliveryCount:  cmp.PhysicalDeliveryCount,
		DuplicateDeliveryCount: cmp.DuplicateDeliveryCount,
		DuplicateRate:          cmp.DuplicateRate,
		MissingCount:           cmp.MissingCount,
		UnexpectedCount:        cmp.UnexpectedCount,
		CorruptedCount:         cmp.CorruptedCount,
		RoutingMismatchCount:   cmp.RoutingMismatch,
		ChildMismatchCount:     cmp.ChildMismatch,
		DetailJSON:             "{}",
	}
}

// FailureSamplesToResults converts in-memory failure samples for normalized storage.
func FailureSamplesToResults(samples []FailureSample) []results.ValidationFailureSample {
	if len(samples) == 0 {
		return nil
	}
	out := make([]results.ValidationFailureSample, len(samples))
	for i, s := range samples {
		out[i] = results.ValidationFailureSample{
			Ordinal:    i + 1,
			Kind:       s.Kind,
			LogicalKey: s.LogicalKey,
			Expected:   s.Expected,
			Actual:     s.Actual,
			Detail:     s.Detail,
		}
	}
	return out
}

// WriteScenarioManifests persists resolved Kubernetes manifest blobs for one scenario run.
func WriteScenarioManifests(w ArtifactsWriter, runID string, manifestDocs map[string]string) error {
	if w == nil {
		return nil
	}
	for name, content := range manifestDocs {
		if name == "image_ref" {
			continue
		}
		if err := w.WriteResolvedManifest(runID, name, []byte(content)); err != nil {
			return err
		}
	}
	return nil
}
