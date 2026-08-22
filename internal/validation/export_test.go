package validation

import "numa-perfman/internal/oracle"

// SchemaDDLForTest exposes schema selection for unit tests.
func SchemaDDLForTest(s oracle.Scenario) string {
	return schemaDDLForScenario(s)
}
