package runmeta

import (
	"context"
	"errors"
	"testing"
)

type stubResolver struct {
	digest string
	err    error
}

func (s stubResolver) ResolveImageDigest(context.Context, string) (string, error) {
	return s.digest, s.err
}

func TestResolveDigestPinnedReference(t *testing.T) {
	d, warn := ResolveDigest(context.Background(), "repo/app@sha256:abc", nil)
	if d != "sha256:abc" || warn != "" {
		t.Fatalf("digest=%q warn=%q", d, warn)
	}
}

func TestResolveDigestResolutionError(t *testing.T) {
	_, warn := ResolveDigest(context.Background(), "repo/app:v1", stubResolver{err: errors.New("boom")})
	if warn == "" {
		t.Fatal("expected warning")
	}
}

func TestUsesMutableTag(t *testing.T) {
	if !UsesMutableTag("quay.io/x/y:v1") {
		t.Fatal("expected mutable")
	}
	if UsesMutableTag("quay.io/x/y@sha256:dead") {
		t.Fatal("digest pin is not mutable tag")
	}
}
