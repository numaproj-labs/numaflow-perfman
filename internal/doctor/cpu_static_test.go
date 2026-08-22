package doctor_test

import (
	"testing"

	"numa-perfman/internal/doctor"
)

func TestStaticKubeletMissingFields(t *testing.T) {
	valid := `cpuManagerPolicy: static
reservedSystemCPUs: "0"
kubeReserved:
  cpu: "1"
cpuManagerPolicyOptions:
  strict-cpu-reservation: "true"
`
	if missing := doctor.StaticKubeletMissingFieldsForTest(valid); len(missing) != 0 {
		t.Fatalf("expected valid config, missing %v", missing)
	}
	incomplete := `cpuManagerPolicy: static
reservedSystemCPUs: "0"
`
	if missing := doctor.StaticKubeletMissingFieldsForTest(incomplete); len(missing) == 0 {
		t.Fatal("expected missing fields")
	}
}
