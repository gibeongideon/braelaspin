// Package game holds the wheel and the spin transaction.
//
// The wheel is a fixed weight table and the RTP is one number. The house
// bankroll plays exactly one role: it vetoes a stake it could not cover,
// BEFORE the draw. It never touches the weights.
//
// That distinction is the whole design. The reference implementation this
// replaces chose uniformly among every segment the bankroll happened to be
// able to afford, which meant the odds drifted with the pool depth and player
// expected value ran at 10x-70x the stake instead of the intended 0.9x.
package game

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Seg is one wheel segment.
type Seg struct {
	// MultBP is the payout multiplier in basis points: 10000 = 1x stake,
	// 0 = a losing segment.
	MultBP int
	// Weight is this segment's share of 10000. The weights sum to exactly
	// 10000, so a single uint32 draw modulo 10000 selects a segment.
	Weight int
}

// WeightTotal is the denominator every weight is expressed against.
const WeightTotal = 10000

// Wheel is the single source of truth for the game's odds.
//
// The client fetches this from GET /v1/game/config and never hardcodes a copy.
// (The reference implementation kept the map in settings.py AND again in its
// JavaScript, relying on humans to keep the two equal.)
//
// Losing segments sit at positions 1, 4, 7 and 10 so the wheel reads as
// alternating win/lose rather than as two dead halves.
//
//	multiplier  segments  weight  probability  RTP contribution
//	0 (lose)        4      6277     62.77%          --
//	1x              2      2000     20.00%        2000 bp
//	2x              2      1200     12.00%        2400 bp
//	5x              1       400      4.00%        2000 bp
//	10x             1       100      1.00%        1000 bp
//	50x             1        20      0.20%        1000 bp
//	200x            1         3      0.03%         600 bp
//	               12     10000    100.00%        9000 bp  -> RTP 90.00%
//
// The player wins something 37.23% of the time.
var Wheel = [12]Seg{
	{MultBP: 0, Weight: 1569},
	{MultBP: 10000, Weight: 1000},
	{MultBP: 20000, Weight: 600},
	{MultBP: 0, Weight: 1569},
	{MultBP: 50000, Weight: 400},
	{MultBP: 10000, Weight: 1000},
	{MultBP: 0, Weight: 1569},
	{MultBP: 100000, Weight: 100},
	{MultBP: 20000, Weight: 600},
	{MultBP: 0, Weight: 1570},
	{MultBP: 500000, Weight: 20},
	{MultBP: 2000000, Weight: 3},
}

// MaxMultBP is the largest multiplier on the wheel. Admission control requires
// the bankroll to cover MaxMultBP x stake, so this number sets the maximum
// stake a given bankroll can accept: at 200x, a KES 100,000 bankroll admits a
// KES 500 stake.
func MaxMultBP() int {
	m := 0
	for _, s := range Wheel {
		if s.MultBP > m {
			m = s.MultBP
		}
	}
	return m
}

// RTPBP returns the return-to-player implied by the table, in basis points.
//
// RTP = sum(multiplier x probability), and probability is Weight/WeightTotal,
// so this is sum(MultBP x Weight) / WeightTotal. For the table above that is
// 90,000,000 / 10,000 = 9000 bp.
func RTPBP() int {
	var weighted int64
	for _, s := range Wheel {
		weighted += int64(s.MultBP) * int64(s.Weight)
	}
	return int(weighted / WeightTotal)
}

// Validate checks the table against itself and against the configured RTP.
// It runs at startup so a typo fails the process in the first 50ms rather than
// quietly changing the house edge in production.
func Validate(wantRTPBP int) error {
	var weightSum, weighted int64
	for i, s := range Wheel {
		if s.Weight <= 0 {
			return fmt.Errorf("game: segment %d has weight %d; every segment must be reachable",
				i+1, s.Weight)
		}
		if s.MultBP < 0 {
			return fmt.Errorf("game: segment %d has negative multiplier %d", i+1, s.MultBP)
		}
		weightSum += int64(s.Weight)
		weighted += int64(s.MultBP) * int64(s.Weight)
	}
	if weightSum != WeightTotal {
		return fmt.Errorf("game: segment weights sum to %d, want exactly %d", weightSum, WeightTotal)
	}
	// weighted is in (bp of multiplier * weight). Dividing by WeightTotal gives
	// the expected payout in bp of the stake, i.e. the RTP.
	gotRTP := weighted / WeightTotal
	if gotRTP != int64(wantRTPBP) {
		return fmt.Errorf("game: wheel table yields RTP %d bp but RTP_BP is configured as %d; "+
			"fix the table or the config, do not ship a wheel whose odds disagree with its stated RTP",
			gotRTP, wantRTPBP)
	}
	// The schema constrains segment_index to BETWEEN 1 AND 12. If the table ever
	// grows or shrinks, every insert would fail the CHECK at run time — on a
	// player's spin. Catch it at startup instead, where it is a deployment
	// error rather than a failed bet.
	if len(Wheel) != 12 {
		return fmt.Errorf(
			"game: wheel has %d segments but the spins.segment_index CHECK allows 1..12; "+
				"add a migration widening the constraint before changing the table size",
			len(Wheel))
	}
	return nil
}

// Picker draws segments. The zero value uses crypto/rand and is what production
// uses; tests substitute a deterministic reader.
type Picker struct {
	// Rand is the entropy source. Nil means crypto/rand.Reader.
	Rand io.Reader
}

// Pick returns a 1-indexed segment and its multiplier.
//
// 1-indexed because that is the wire format the client animates against: it
// rotates the wheel so segment `index` lands under a pointer fixed at 12
// o'clock. The client never computes an outcome; it renders the one we chose.
func (p Picker) Pick() (index int, multBP int, err error) {
	src := p.Rand
	if src == nil {
		src = rand.Reader
	}
	var b [4]byte
	if _, err := io.ReadFull(src, b[:]); err != nil {
		// There is no safe fallback here. Drawing a prize from a degraded
		// entropy source is worse than refusing to spin, so this propagates
		// and the spin returns an error.
		return 0, 0, fmt.Errorf("game: entropy source unavailable: %w", err)
	}

	// Modulo bias over a uniform uint32 into 10000 buckets is about 1 part in
	// 430,000 -- five orders of magnitude below the noise of any feasible audit
	// and irrelevant against a 10% house edge. Noted so nobody has to
	// rediscover it; rejection sampling would be correct and is not worth it.
	r := int(binary.BigEndian.Uint32(b[:]) % WeightTotal)

	for i, s := range Wheel {
		if r < s.Weight {
			return i + 1, s.MultBP, nil
		}
		r -= s.Weight
	}
	// Unreachable: Validate guarantees the weights sum to WeightTotal.
	return 0, 0, fmt.Errorf("game: wheel selection fell through with remainder %d "+
		"(weights do not sum to %d -- Validate should have caught this at startup)", r, WeightTotal)
}

// Payout is the win for a stake at a given multiplier.
//
// Integer division, truncating, matching the spins_payout_exact CHECK in the
// schema exactly. Both sides must round the same way or inserts will fail.
func Payout(stakeCents int64, multBP int) int64 {
	return stakeCents * int64(multBP) / 10000
}

// MaxExposure is the largest payout this stake could produce. Admission
// control compares it against the free bankroll.
// MaxSafeStake is the largest stake for which stake x MaxMultBP cannot
// overflow int64. Above it the arithmetic below is meaningless, so config
// refuses to start (see ValidateStakeCeiling).
func MaxSafeStake() int64 {
	m := int64(MaxMultBP())
	if m == 0 {
		return math.MaxInt64
	}
	return math.MaxInt64 / m
}

// MaxExposure is the largest payout this stake could produce -- what the
// bankroll must be able to cover before the draw.
//
// The overflow guard is not theoretical decoration. stake x 2,000,000 wraps
// negative above ~4.6e12 cents, and a NEGATIVE exposure makes the admission
// check `bankroll < exposure` false, so a stake the bankroll cannot possibly
// pay would be admitted. Returning MaxInt64 instead fails closed: such a
// stake is vetoed by every finite bankroll.
func MaxExposure(stakeCents int64) int64 {
	if stakeCents > MaxSafeStake() {
		return math.MaxInt64
	}
	return Payout(stakeCents, MaxMultBP())
}

// MaxAffordableStake is the largest stake the given bankroll can safely accept.
// MaxAffordableStake is the largest stake this bankroll can cover.
func MaxAffordableStake(bankrollCents int64) int64 {
	m := int64(MaxMultBP())
	if m == 0 {
		return 0
	}
	if bankrollCents <= 0 {
		return 0
	}
	// bankroll x 10000 overflows above ~9.2e14 cents and would return a
	// NEGATIVE maximum stake. Above that threshold divide first: the answer
	// loses at most one cent of precision, on a bankroll of KES 9 trillion.
	var stake int64
	if bankrollCents > math.MaxInt64/10000 {
		stake = bankrollCents / m * 10000
	} else {
		stake = bankrollCents * 10000 / m
	}
	// Never advertise a stake the exposure calculation cannot represent. Without
	// this clamp a vast bankroll would report a maximum that MaxExposure then
	// vetoes, so GET /game/config would promise a bet the server refuses.
	if limit := MaxSafeStake(); stake > limit {
		return limit
	}
	return stake
}

// ValidateStakeCeiling refuses a configured maximum stake whose worst-case
// payout could overflow. Called at startup beside Validate, so a misplaced
// zero in MAX_STAKE_CENTS fails the process instead of corrupting a draw.
func ValidateStakeCeiling(maxStakeCents int64) error {
	if maxStakeCents <= 0 {
		return fmt.Errorf("game: MAX_STAKE_CENTS must be positive, got %d", maxStakeCents)
	}
	if limit := MaxSafeStake(); maxStakeCents > limit {
		return fmt.Errorf(
			"game: MAX_STAKE_CENTS=%d exceeds the arithmetically safe ceiling of %d cents "+
				"(a stake above this overflows stake x %d bp and would defeat admission control)",
			maxStakeCents, limit, MaxMultBP())
	}
	return nil
}

// Segment describes one wheel segment for the client. Weights are included so
// the odds are inspectable by anyone who wants to check them.
type Segment struct {
	Index    int `json:"index"`
	MultBP   int `json:"multiplier_bp"`
	WeightBP int `json:"weight_bp"`
}

// Segments renders the wheel for GET /v1/game/config.
func Segments() []Segment {
	out := make([]Segment, 0, len(Wheel))
	for i, s := range Wheel {
		out = append(out, Segment{Index: i + 1, MultBP: s.MultBP, WeightBP: s.Weight})
	}
	return out
}
