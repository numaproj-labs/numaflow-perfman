package runmeta

import (
	"context"
	"fmt"
	"strings"
)

// DigestResolver resolves an image reference to an immutable digest when possible.
type DigestResolver interface {
	ResolveImageDigest(ctx context.Context, imageRef string) (string, error)
}

// ResolveDigest resolves digest and returns a human-readable warning when resolution fails.
func ResolveDigest(ctx context.Context, imageRef string, resolver DigestResolver) (digest string, warning string) {
	if imageRef == "" {
		return "", ""
	}
	if d := digestInReference(imageRef); d != "" {
		return d, ""
	}
	if resolver == nil {
		return "", ""
	}
	d, err := resolver.ResolveImageDigest(ctx, imageRef)
	if err != nil {
		return "", fmt.Sprintf("image digest resolution failed for %q: %v", imageRef, err)
	}
	if d == "" && UsesMutableTag(imageRef) {
		return "", fmt.Sprintf("image %q uses a mutable tag and no digest was resolved", imageRef)
	}
	return d, ""
}

// digestInReference returns the digest portion when ref is pinned by digest.
func digestInReference(imageRef string) string {
	if at := strings.LastIndex(imageRef, "@sha256:"); at >= 0 {
		return imageRef[at+1:]
	}
	return ""
}

// UsesMutableTag reports whether ref appears to use a tag rather than a digest pin.
func UsesMutableTag(imageRef string) bool {
	if imageRef == "" || strings.Contains(imageRef, "@sha256:") {
		return false
	}
	return strings.Contains(imageRef, ":")
}
