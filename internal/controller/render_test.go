package controller_test

import (
	"strings"
	"testing"

	"numa-perfman/internal/controller"
)

func TestValidateImageReference(t *testing.T) {
	valid := []string{
		"quay.io/numaproj/numaflow:v1.7.0",
		"numaflow:local",
		"localhost/numaflow:feature-123",
		"quay.io/numaproj/numaflow@sha256:abc123",
	}
	for _, ref := range valid {
		if err := controller.ValidateImageReference(ref); err != nil {
			t.Fatalf("%q: %v", ref, err)
		}
	}
	invalid := []string{"v1.2", "", "latest"}
	for _, ref := range invalid {
		if err := controller.ValidateImageReference(ref); err == nil {
			t.Fatalf("expected error for %q", ref)
		}
	}
}

func TestRenderControllerManifests(t *testing.T) {
	img := "quay.io/numaproj/numaflow:v1.8.0"
	ns := "numaflow-perf-deadbeef"
	set, err := controller.Render(controller.RenderInput{Namespace: ns, ImageReference: img})
	if err != nil {
		t.Fatal(err)
	}
	all := set.Concat()
	if count, want := strings.Count(all, "\n---\n"), len(set.ApplyDocuments())-1; count != want {
		t.Fatalf("document separators = %d, want %d", count, want)
	}
	if !strings.HasSuffix(all, "\n") {
		t.Fatal("manifest bundle must end with a newline")
	}
	if !strings.Contains(all, img) {
		t.Fatal("missing image")
	}
	if !strings.Contains(all, `"NUMAFLOW_IMAGE"`) || !strings.Contains(all, img) {
		t.Fatal("missing NUMAFLOW_IMAGE")
	}
	if strings.Contains(all, `"perfman.numaproj.io/numaflow-image": "`+img+`"`) {
		t.Fatal("complete image reference is not a valid Kubernetes label value")
	}
	if !strings.Contains(all, `"perfman.numaproj.io/numaflow-image-ref": "`+img+`"`) {
		t.Fatal("full image reference must be retained as an annotation")
	}
	if !strings.Contains(all, controller.ImagePullPolicy) {
		t.Fatal("missing pull policy")
	}
	if !strings.Contains(all, controller.ControllerConfigMap) {
		t.Fatal("missing controller config map")
	}
	if !strings.Contains(all, controller.ConfigMapName) {
		t.Fatal("missing cmd params config map")
	}
	if strings.Contains(all, "Always") {
		t.Fatal("should not use Always pull policy")
	}
}
