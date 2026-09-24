package main

import (
	"strings"
	"testing"
)

// defaultRepetitionCfg is the compiled default loop-guard policy used by the
// detector unit tests.
func defaultRepetitionCfg() RepetitionConfig {
	return RepetitionConfig{
		Enabled: true,
		Repeats: defaultRepetitionRepeats,
		MinLen:  defaultRepetitionMinLen,
		MaxLen:  defaultRepetitionMaxLen,
	}
}

// TestRepetitionDetectorCatchesToolCallLoop is the headline contract: the
// stream that was the reason for the feature — "Tool call. Tool call. …" —
// trips the guard on the K-th consecutive identical normalized fragment, no
// matter how the text is chunked or cased.
func TestRepetitionDetectorCatchesToolCallLoop(t *testing.T) {
	k := defaultRepetitionRepeats
	d := newRepetitionDetector(defaultRepetitionCfg())
	// The first K-1 consecutive fragments must not trip yet.
	for i := 0; i < k-1; i++ {
		if d.Detect("Tool call.") {
			t.Fatalf("detector tripped after %d fragments (K = %d)", i+1, k)
		}
	}
	// The K-th consecutive fragment trips the guard.
	if !d.Detect(" tool   call. ") {
		t.Fatal("detector must trip on the K-th consecutive normalized fragment")
	}
	// It stays tripped on subsequent repeats (the relay halts on the first
	// trip; this documents that Detect keeps reporting the loop).
	if !d.Detect("Tool call.") {
		t.Fatal("detector must keep reporting the loop once K repeats are reached")
	}
}

// TestRepetitionDetectorSurvivesNaturalProse pins the false-positive guard: a
// long non-repeating answer (incl. single repeats of ordinary words, which
// fall below MinLen or below K) never trips the detector.
func TestRepetitionDetectorSurvivesNaturalProse(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionCfg())
	prose := strings.Join([]string{
		"Sure. The load balancer uses power of two choices:",
		"draw two random healthy candidates, then promote the one with fewer",
		"in-flight branches. Under concurrency it distributes evenly without",
		"latency feedback. The health window is five minutes and the error budget",
		"is twenty percent, so a provider above that share of health errors is",
		"excluded from the choice entirely. Reasoning models may pause without",
		"emitting events for a long time, so the idle watchdog is generous and",
		"the takeover threshold defaults to ninety seconds.", "Done.",
	}, " ")
	if d.Detect(prose) {
		t.Fatal("natural prose must not trip the loop guard")
	}
}

// TestRepetitionDetectorIgnoresShortRepeats pins MinLen: repeats of fragments
// shorter than MinLen (natural fillers, barrier characters, laughter) are not
// loops.
func TestRepetitionDetectorIgnoresShortRepeats(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionCfg())
	for i := 0; i < 20; i++ {
		if d.Detect("yes") {
			t.Fatalf("short unit \"yes\" (len 3 < min_len 6) must not trip, iteration %d", i)
		}
	}
	d = newRepetitionDetector(defaultRepetitionCfg())
	for i := 0; i < 20; i++ {
		if d.Detect("}") {
			t.Fatalf("barrier character run must not trip, iteration %d", i)
		}
	}
	d = newRepetitionDetector(defaultRepetitionCfg())
	for i := 0; i < 20; i++ {
		if d.Detect("haha ") {
			t.Fatalf("short laughter unit (len 4 < min_len 6) must not trip, iteration %d", i)
		}
	}
}

// TestRepetitionDetectorWindowBoundsUnits pins MaxLen: a fragment longer than
// MaxLen can never be a unit, so a few repeats of a very long sentence do not
// trip (a real loop repeats a short fragment thousands of times, not a
// 300-char sentence four times).
func TestRepetitionDetectorWindowBoundsUnits(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionCfg())
	unit := strings.Repeat("x", defaultRepetitionMaxLen+50)
	for i := 0; i < defaultRepetitionRepeats+2; i++ {
		if d.Detect(unit + " ") {
			t.Fatalf("over-MaxLen unit must not trip, iteration %d", i)
		}
	}
}

// TestRepetitionDetectorTripsOnSecondChunk tests chunking robustness: the
// loop fragments may be split arbitrarily across chunks (even mid-token); the
// detector must rejoin them and trip on the K-th identical unit.
func TestRepetitionDetectorTripsOnSecondChunk(t *testing.T) {
	k := defaultRepetitionRepeats
	d := newRepetitionDetector(defaultRepetitionCfg())
	// K-1 whole fragments must not trip.
	for i := 0; i < k-1; i++ {
		if d.Detect("Tool call. ") {
			t.Fatalf("detector must not trip below K, iteration %d", i)
		}
	}
	// The K-th fragment arrives split across a chunk boundary mid-token;
	// normalization must rejoin it and trip only once the unit completes.
	if d.Detect("Tool cal") {
		t.Fatal("the split fragment must not trip before its final token arrives")
	}
	if !d.Detect("l. ") {
		t.Fatal("detector must rejoin the split fragment and trip on the K-th unit")
	}
}

// TestRepetitionDetectorDisabledPinsEnabled: a detector built from a disabled
// policy never trips and never accumulates.
func TestRepetitionDetectorDisabled(t *testing.T) {
	cfg := defaultRepetitionCfg()
	cfg.Enabled = false
	d := newRepetitionDetector(cfg)
	for i := 0; i < 20; i++ {
		if d.Detect("Tool call. ") {
			t.Fatalf("disabled detector must never trip, iteration %d", i)
		}
	}
}

// TestRepetitionDetectorReset clears state so a fresh lane starts clean.
func TestRepetitionDetectorReset(t *testing.T) {
	k := defaultRepetitionRepeats
	d := newRepetitionDetector(defaultRepetitionCfg())
	for i := 0; i < k; i++ {
		d.Detect("Tool call. ")
	}
	if !d.Detect("Tool call.") {
		t.Fatal("precondition: detector should be tripped")
	}
	d.Reset()
	// After Reset the first K-1 repeats must not trip; the K-th does.
	for i := 0; i < k-1; i++ {
		if d.Detect("Tool call. ") {
			t.Fatalf("after Reset the detector must start clean, iteration %d", i)
		}
	}
	if !d.Detect("Tool call. ") {
		t.Fatal("after Reset + K repeats the detector must trip again")
	}
}

// TestRepeatedRunUnit captures the pure comparison primitive: a tail of K
// identical units trips for any unit length in range.
func TestRepeatedRun(t *testing.T) {
	if !repeatedRun("abc abc abc abc", 4, 3, 10) {
		t.Fatal("4x 'abc' (len 3) must be a repeated run")
	}
	if repeatedRun("abc abc abc", 4, 3, 10) {
		t.Fatal("3x 'abc' is not 4 repeats")
	}
	if repeatedRun("abc abd abc abc", 4, 3, 10) {
		t.Fatal("a break in the run must not count as repetition")
	}
	if !repeatedRun("the quick the quick the quick the quick", 4, 4, 10) {
		t.Fatal("4 identical 9-char units must be detected as a run")
	}
	if repeatedRun("hello world is fine here now", 4, 2, 5) {
		t.Fatal("distinct tail must not be a repeated run")
	}
}
