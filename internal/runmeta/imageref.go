package runmeta

import "strings"

// ImageTagSegment returns the tag portion of an OCI reference for namespace and lock keys.
// Digest-pinned refs become sha256-<first 12 hex chars>.
func ImageTagSegment(imageRef string) string {
	ref := strings.TrimSpace(imageRef)
	if ref == "" {
		return ""
	}
	if at := strings.LastIndex(ref, "@sha256:"); at >= 0 {
		hex := ref[at+len("@sha256:"):]
		hex = strings.TrimPrefix(hex, "sha256:")
		hex = strings.ToLower(hex)
		if len(hex) > 12 {
			hex = hex[:12]
		}
		if hex == "" {
			return ""
		}
		return "sha256-" + hex
	}
	if slash := strings.LastIndexByte(ref, '/'); slash >= 0 {
		ref = ref[slash+1:]
	}
	if colon := strings.LastIndexByte(ref, ':'); colon >= 0 && colon < len(ref)-1 {
		return ref[colon+1:]
	}
	return ref
}

// DisplayImageTag returns the tag label for reports and charts.
// Digest-pinned refs return the full reference string.
func DisplayImageTag(imageRef string) string {
	ref := strings.TrimSpace(imageRef)
	if strings.Contains(ref, "@") {
		return ref
	}
	if slash := strings.LastIndexByte(ref, '/'); slash >= 0 {
		ref = ref[slash+1:]
	}
	if colon := strings.LastIndexByte(ref, ':'); colon >= 0 && colon < len(ref)-1 {
		return ref[colon+1:]
	}
	return ref
}
