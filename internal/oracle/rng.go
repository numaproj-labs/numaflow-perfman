package oracle

import (
	"fmt"
	"math/rand/v2"
)

func newRNG(seed uint64) *rand.Rand {
	// Derive two PCG streams from one user seed for stable, reproducible sequences.
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
}

func randomUUID(rng *rand.Rand) string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rng.UintN(256))
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
