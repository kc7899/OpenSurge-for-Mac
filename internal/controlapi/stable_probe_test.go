package controlapi

import (
	"context"
	"os"
	"testing"
)

func TestStableProbeOnlyAcceptsFixedTargets(t *testing.T) {
	for _, input := range []StableProbeInput{{Target: -1}, {Target: 5}, {CandidateID: "arbitrary", Target: 3}} {
		if _, err := (DirectRunner{}).ProbeStable(context.Background(), "/must-not-read", input); err == nil {
			t.Fatal("unsafe probe accepted", input)
		}
	}
	if os.Geteuid() != 0 {
		if _, err := (DirectRunner{}).ProbeStable(context.Background(), "/must-not-read", StableProbeInput{Target: 0, CandidateID: "node"}); err == nil {
			t.Fatal("physical binding allowed outside helper")
		}
	}
}
