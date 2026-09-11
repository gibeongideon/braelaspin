package game

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestWheelWeightsSumToTotal(t *testing.T) {
	sum := 0
	for i, s := range Wheel {
		if s.Weight <= 0 {
			t.Errorf("segment %d has weight %d; every segment must be reachable", i+1, s.Weight)
		}
		sum += s.Weight
	}
	if sum != WeightTotal {
		t.Fatalf("weights sum to %d, want %d", sum, WeightTotal)
	}
}

func TestWheelRTPIsExactly9000BP(t *testing.T) {
	if got := RTPBP(); got != 9000 {
		t.Fatalf("RTPBP() = %d, want 9000 (90.00%% return to player)", got)
	}
}

func TestValidateAcceptsTheShippedTable(t *testing.T) {
	if err := Validate(9000); err != nil {
		t.Fatalf("Validate(9000) on the shipped table: %v", err)
	}
}

func TestValidateRejectsMismatchedRTP(t *testing.T) {
	// The point of Validate: a table whose odds disagree with the stated RTP
	// must not start. This is the guard against silently changing the house
	// edge by editing one weight.
	if err := Validate(8500); err == nil {
		t.Fatal("Validate(8500) accepted a table that yields 9000bp; it must refuse")
	}
}

func TestValidateRejectsBadTables(t *testing.T) {
	orig := Wheel
	t.Cleanup(func() { Wheel = orig })

	cases := []struct {
		name  string
		mutate func()
	}{
		{"zero weight makes a segment unreachable", func() { Wheel[3].Weight = 0 }},
		{"negative weight", func() { Wheel[3].Weight = -5 }},
		{"weights no longer sum to 10000", func() { Wheel[0].Weight = 1568 }},
		{"negative multiplier", func() { Wheel[2].MultBP = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Wheel = orig
			tc.mutate()
			if err := Validate(9000); err == nil {
				t.Errorf("Validate accepted a table with %s", tc.name)
			}
		})
	}
}

func TestMaxMultAndAffordableStake(t *testing.T) {
	if got := MaxMultBP(); got != 2_000_000 {
		t.Fatalf("MaxMultBP() = %d, want 2000000 (200x)", got)
	}
	// A KES 100,000 bankroll (10,000,000 cents) should admit a KES 500 stake.
	if got := MaxAffordableStake(10_000_000); got != 50_000 {
		t.Errorf("MaxAffordableStake(KES 100,000) = %d cents, want 50000 (KES 500)", got)
	}
	// Exposure must never exceed what admission control checked.
	if got := MaxExposure(50_000); got != 10_000_000 {
		t.Errorf("MaxExposure(KES 500) = %d, want 10000000 (KES 100,000)", got)
	}
}

func TestPayoutMatchesSchemaRounding(t *testing.T) {
	// The spins_payout_exact CHECK computes stake * multiplier_bp / 10000 with
	// Postgres integer division, which truncates. Go must truncate identically
	// or every insert with a remainder will be rejected.
	cases := []struct{ stake int64; mult int; want int64 }{
		{500, 0, 0},
		{500, 10000, 500},
		{500, 20000, 1000},
		{500, 2000000, 100000},
		{333, 50000, 1665},
		{1, 20000, 2},
		{7, 10000, 7},
		{333, 10000, 333},
	}
	for _, c := range cases {
		if got := Payout(c.stake, c.mult); got != c.want {
			t.Errorf("Payout(%d, %d) = %d, want %d", c.stake, c.mult, got, c.want)
		}
	}
}

// fixedReader feeds Pick a chosen uint32 so we can target an exact segment.
type fixedReader struct{ v uint32 }

func (f fixedReader) Read(p []byte) (int, error) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], f.v)
	return copy(p, b[:]), nil
}

func TestPickSelectsTheSegmentTheRollLandsIn(t *testing.T) {
	// Walk the cumulative boundaries and confirm the first and last roll of
	// each segment's range map to that segment.
	cum := 0
	for i, s := range Wheel {
		first, last := cum, cum+s.Weight-1
		for _, roll := range []int{first, last} {
			idx, mult, err := Picker{Rand: fixedReader{uint32(roll)}}.Pick()
			if err != nil {
				t.Fatalf("roll %d: %v", roll, err)
			}
			if idx != i+1 {
				t.Errorf("roll %d selected segment %d, want %d", roll, idx, i+1)
			}
			if mult != s.MultBP {
				t.Errorf("roll %d gave multiplier %d, want %d", roll, mult, s.MultBP)
			}
		}
		cum += s.Weight
	}
}

func TestPickIsOneIndexed(t *testing.T) {
	idx, _, err := Picker{Rand: fixedReader{0}}.Pick()
	if err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Fatalf("a zero roll gave index %d; the wire format is 1-indexed so the "+
			"client can rotate segment N under a pointer at 12 o'clock", idx)
	}
}

func TestPickFailsClosedOnEntropyFailure(t *testing.T) {
	// Drawing a prize from a degraded entropy source is worse than refusing to
	// spin, so Pick must return an error rather than fall back to anything.
	_, _, err := Picker{Rand: bytes.NewReader(nil)}.Pick()
	if err == nil {
		t.Fatal("Pick succeeded with an empty entropy source; it must fail closed")
	}
}

func TestPickDistributionMatchesWeights(t *testing.T) {
	// Chi-square goodness of fit against the declared weights. This is the test
	// that would catch a biased selector, an off-by-one in the cumulative walk,
	// or a table edited without updating the weights.
	const n = 2_000_000
	counts := make([]int, len(Wheel))
	p := Picker{} // crypto/rand
	for i := 0; i < n; i++ {
		idx, _, err := p.Pick()
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		counts[idx-1]++
	}

	var chi2 float64
	for i, s := range Wheel {
		expected := float64(n) * float64(s.Weight) / float64(WeightTotal)
		d := float64(counts[i]) - expected
		chi2 += d * d / expected
		if counts[i] == 0 {
			t.Errorf("segment %d (weight %d) never came up in %d draws", i+1, s.Weight, n)
		}
	}

	// 11 degrees of freedom. The 0.999 quantile is 34.53; exceeding it means
	// roughly a 1-in-1000 false alarm, so a failure here is worth investigating
	// rather than re-running.
	const critical = 34.53
	if chi2 > critical {
		t.Errorf("chi-square = %.2f exceeds %.2f for 11 df; the observed "+
			"distribution does not match the declared weights\ncounts: %v", chi2, critical, counts)
	}
	t.Logf("chi-square = %.2f (critical %.2f, 11 df) over %d draws", chi2, critical, n)
}

func TestRealisedRTPConvergesOnConfiguredRTP(t *testing.T) {
	// Simulate real play and confirm the money actually behaves as the table
	// claims. This is the end-to-end check on the economics: if this drifts,
	// the house edge is not what the business thinks it is.
	const (
		n     = 2_000_000
		stake = int64(1_000) // KES 10
	)
	var staked, paid int64
	p := Picker{}
	for i := 0; i < n; i++ {
		_, mult, err := p.Pick()
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		staked += stake
		paid += Payout(stake, mult)
	}

	realised := float64(paid) / float64(staked) * 10000 // in bp
	want := float64(RTPBP())

	// Per-spin variance is dominated by the 200x segment. sigma per spin is
	// about 2.4x the stake, so the standard error on the mean over n spins is
	// 2.4/sqrt(n) in units of stake -> in bp, 24000/sqrt(n).
	stdErrBP := 24000.0 / math.Sqrt(float64(n))
	tol := 4 * stdErrBP // 4 sigma: flaky less than 1 run in 15,000

	if math.Abs(realised-want) > tol {
		t.Errorf("realised RTP %.1f bp differs from configured %.0f bp by more than %.1f bp (4 sigma)",
			realised, want, tol)
	}
	t.Logf("realised RTP %.1f bp vs configured %.0f bp (tolerance +/-%.1f bp over %d spins)",
		realised, want, tol, n)
}

func TestHouseEdgeCoversRakeAndReferral(t *testing.T) {
	// The economics the plan commits to: a 10% edge split 5% rake, 2%
	// referral, 3% bankroll growth. If the table ever changes, this asserts
	// the edge is still big enough to pay what we promise out of it.
	edge := 10000 - RTPBP()
	const rakeBP, referralBP = 500, 200
	if rakeBP+referralBP > edge {
		t.Fatalf("rake %d bp + referral %d bp exceeds the %d bp house edge", rakeBP, referralBP, edge)
	}
	t.Logf("edge %d bp = rake %d + referral %d + bankroll growth %d",
		edge, rakeBP, referralBP, edge-rakeBP-referralBP)
}
