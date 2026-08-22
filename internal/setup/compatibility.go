package setup

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DefaultTestedMajorMinor is the Numaflow major.minor tag documented as exercised
// together in this repo's kind workflows (see docs/compatibility-matrix.md).
// It is guidance only, not a semver compatibility guarantee.
const DefaultTestedMajorMinor = "1.8"

var numaflowTagPattern = regexp.MustCompile(`:v?(\d+)\.(\d+)(?:\.|$)`)

// VersionTag holds a parsed major.minor from an OCI reference tag.
type VersionTag struct {
	Major int
	Minor int
	Raw   string
}

// ParseImageMajorMinor extracts major.minor from refs like quay.io/numaproj/numaflow:v1.8.0.
// Returns ok=false when no version tag pattern is found (local tags, digests).
func ParseImageMajorMinor(imageRef string) (VersionTag, bool) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return VersionTag{}, false
	}
	m := numaflowTagPattern.FindStringSubmatch(imageRef)
	if m == nil {
		return VersionTag{}, false
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	return VersionTag{Major: maj, Minor: min, Raw: fmt.Sprintf("%d.%d", maj, min)}, true
}

// CompatibilityWarning is a non-fatal setup/doctor advisory.
type CompatibilityWarning struct {
	Code    string
	Message string
}

// CompatibilityWarnings evaluates UI and optional controller images against the
// documented tested major.minor range and each other. Empty testedMajorMinor uses
// DefaultTestedMajorMinor.
func CompatibilityWarnings(uiImage, controllerImage, testedMajorMinor string) []CompatibilityWarning {
	if strings.TrimSpace(testedMajorMinor) == "" {
		testedMajorMinor = DefaultTestedMajorMinor
	}
	tested, testedOK := ParseImageMajorMinor("placeholder:v" + testedMajorMinor + ".0")
	if !testedOK {
		return nil
	}

	var out []CompatibilityWarning
	if ui, ok := ParseImageMajorMinor(uiImage); ok {
		if ui.Major != tested.Major || ui.Minor != tested.Minor {
			out = append(out, CompatibilityWarning{
				Code: "ui.outside_tested_range",
				Message: fmt.Sprintf(
					"central UI image tag %s differs from documented tested range v%s.x (see docs/compatibility-matrix.md); verify UI/CRD behavior before release gating",
					ui.Raw, testedMajorMinor,
				),
			})
		}
	}
	if ctrl, ok := ParseImageMajorMinor(controllerImage); ok {
		if ctrl.Major != tested.Major || ctrl.Minor != tested.Minor {
			out = append(out, CompatibilityWarning{
				Code: "controller.outside_tested_range",
				Message: fmt.Sprintf(
					"controller image tag %s differs from documented tested range v%s.x; confirm CRDs and validation scenarios against your matrix",
					ctrl.Raw, testedMajorMinor,
				),
			})
		}
		if ui, uiOK := ParseImageMajorMinor(uiImage); uiOK {
			if ui.Major != ctrl.Major || ui.Minor != ctrl.Minor {
				out = append(out, CompatibilityWarning{
					Code: "ui_controller.minor_mismatch",
					Message: fmt.Sprintf(
						"central UI tag %s and controller tag %s differ; UI read-only inspection may not match controller CRD generation (see docs/compatibility-matrix.md)",
						ui.Raw, ctrl.Raw,
					),
				})
			}
		}
	}
	return out
}
