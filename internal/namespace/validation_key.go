package namespace

import (
	"fmt"

	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
)

const (
	// PrefixValidation is the namespace prefix for validation runs (validation-<scenario>-<tag>).
	PrefixValidation = "validation-"
	// PrefixValidationLegacy is the retired UUID-based validation namespace prefix.
	PrefixValidationLegacy = "numaflow-validation-"
)

// ValidationKey identifies a validation namespace and lock identity from scenario and image reference.
type ValidationKey struct {
	Scenario     string
	ImageTag     string // raw tag segment before sanitization
	ImageTagSafe string // sanitized tag segment
	Namespace    string
	LockFileName string // e.g. validation-map-v1-8-0.lock
}

// ParseValidationKey derives namespace and lock identity for a validation scenario and image reference.
func ParseValidationKey(scenarioID, imageRef string) (ValidationKey, error) {
	if _, err := scenario.LookupValidation(scenarioID); err != nil {
		return ValidationKey{}, fmt.Errorf("validation scenario %q: %w", scenarioID, err)
	}
	tag := runmeta.ImageTagSegment(imageRef)
	if tag == "" {
		return ValidationKey{}, fmt.Errorf("image reference %q has no tag or digest", imageRef)
	}
	scenarioSafe := sanitizeDNS1123(scenarioID)
	tagSafe := sanitizeDNS1123(tag)
	if scenarioSafe == "" || tagSafe == "" {
		return ValidationKey{}, fmt.Errorf("scenario %q and image %q produce empty namespace segments", scenarioID, imageRef)
	}
	ns := fitNamespace(PrefixValidation, PrefixValidation+scenarioSafe+"-"+tagSafe)
	lockName := PrefixValidation + scenarioSafe + "-" + tagSafe + ".lock"
	return ValidationKey{
		Scenario:     scenarioID,
		ImageTag:     tag,
		ImageTagSafe: tagSafe,
		Namespace:    ns,
		LockFileName: lockName,
	}, nil
}
