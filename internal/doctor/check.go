package doctor

import "fmt"

// Status is the outcome of a single check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
	StatusSkip Status = "skip"
)

// Check is one read-only preflight evaluation.
type Check struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Blocker bool   `json:"blocker"`
	Message string `json:"message"`
}

// Result aggregates checks. Error is non-nil when any blocker failed.
type Result struct {
	Checks []Check `json:"checks"`
}

// Blockers returns failed blocker checks.
func (r Result) Blockers() []Check {
	var out []Check
	for _, c := range r.Checks {
		if c.Blocker && c.Status == StatusFail {
			out = append(out, c)
		}
	}
	return out
}

// Error implements error for blocker failures.
func (r Result) Error() error {
	blockers := r.Blockers()
	if len(blockers) == 0 {
		return nil
	}
	return fmt.Errorf("doctor found %d blocker(s): %s", len(blockers), blockers[0].Message)
}

// Passed reports whether all blocker checks passed.
func (r Result) Passed() bool {
	return r.Error() == nil
}

func pass(id, name, msg string) Check {
	return Check{ID: id, Name: name, Status: StatusPass, Message: msg}
}

func fail(id, name, msg string, blocker bool) Check {
	return Check{ID: id, Name: name, Status: StatusFail, Blocker: blocker, Message: msg}
}

func warn(id, name, msg string) Check {
	return Check{ID: id, Name: name, Status: StatusWarn, Message: msg}
}

func skip(id, name, msg string) Check {
	return Check{ID: id, Name: name, Status: StatusSkip, Message: msg}
}
