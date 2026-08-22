package controller_test

import (
	"testing"

	"numa-perfman/internal/controller"
)

// Ensures run-scoped controller rendering remains namespace-local (deploy/run/controller).
func TestDeployRunControllerRender(t *testing.T) {
	_, err := controller.Render(controller.RenderInput{
		Namespace:      "numaflow-perf-deadbeef",
		ImageReference: "quay.io/numaproj/numaflow:v1.8.0",
	})
	if err != nil {
		t.Fatal(err)
	}
}
