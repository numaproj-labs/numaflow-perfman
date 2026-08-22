package doctor_test

import (
	"testing"

	"numa-perfman/internal/doctor"
)

func TestResultErrorOnBlockers(t *testing.T) {
	pass := doctor.Result{Checks: []doctor.Check{{ID: "ok", Status: doctor.StatusPass}}}
	if pass.Error() != nil {
		t.Fatal("pass result should not error")
	}
	fail := doctor.Result{Checks: []doctor.Check{
		{ID: "x", Status: doctor.StatusFail, Blocker: true, Message: "blocked"},
	}}
	if fail.Error() == nil {
		t.Fatal("expected blocker error")
	}
}
