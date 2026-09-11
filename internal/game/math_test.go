package game_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"testing"

	"github.com/dibon/braelaspin/internal/game"
	"github.com/dibon/braelaspin/internal/testutil"
)

// This file is the definitive proof that the money arithmetic is exact.
//
// The rule being proved, in the product owner's words:
//
//	"if the spinner hits 5, the win is bet * multiplier"
//
// Everything here is about that identity and the accounting that follows from
// it. Where a value could conceivably differ between Go and Postgres, the test
// computes it BOTH ways and compares, rather than trusting that two
// implementations of one formula agree.

// ── 1. the headline identity ────────────────────────────────────────────────

func TestPayoutIsExactlyStakeTimesMultiplier(t *testing.T) {
	multipliers := []struct{ x, bp int }{
		{0, 0}, {1, 10_000}, {2, 20_000}, {5, 50_000},
		{10, 100_000}, {50, 500_000}, {200, 2_000_000},
	}
	stakes := []int64{500, 1_000, 2_500, 5_000, 10_000, 50_000, 123_456, 5_000_000}

	for _, m := range multipliers {
		for _, stake := range stakes {
			got := game.Payout(stake, m.bp)
			want := stake * int64(m.x) // the plain-English rule
			if got != want {
				t.Errorf("Payout(%d, %dx) = %d, want %d  (bet × multiplier)",
					stake, m.x, got, want)
			}
		}
	}
}

func TestFiveXOnATenShillingBet(t *testing.T) {
	// The exact example from the brief, spelled out.
	const stake = 1_000 // KES 10.00
	payout := game.Payout(stake, 50_000)

	if payout != 5_000 {
		t.Fatalf("payout = %d, want 5000 (KES 50 = KES 10 × 5)", payout)
	}
	// The stake is consumed and the payout credited, so the net change is
	// payout - stake. This gross model is what makes the RTP table mean what
	// it says: Σ(multiplier × probability) assumes the stake buys the payout.
	if net := payout - stake; net != 4_000 {
		t.Errorf("net = %d, want 4000 (KES 40 gained on a KES 10 bet)", net)
	}
}

func TestOneXIsExactlyBreakEven(t *testing.T) {
	// If 1x were not break-even the whole RTP calculation would be wrong.
	for _, stake := range []int64{500, 999, 1_000, 33_333, 5_000_000} {
		if p := game.Payout(stake, 10_000); p != stake {
			t.Errorf("Payout(%d, 1x) = %d, want %d — 1x returns the stake exactly",
				stake, p, stake)
		}
	}
}

func TestLosingSegmentPaysExactlyZero(t *testing.T) {
	for _, stake := range []int64{500, 1_234, 5_000_000} {
		if p := game.Payout(stake, 0); p != 0 {
			t.Errorf("Payout(%d, 0x) = %d, want 0", stake, p)
		}
	}
}

// ── 2. rounding ─────────────────────────────────────────────────────────────

// Every multiplier on the live wheel is a whole number of times 10000, so no
// stake can ever produce a remainder and no rounding occurs in production.
// This test pins that: whoever adds a fractional multiplier (1.5x = 15000bp)
// sees it fail and has to sign off on truncation deliberately.
func TestEveryWheelMultiplierDividesExactly(t *testing.T) {
	for i, seg := range game.Wheel {
		if seg.MultBP%10_000 != 0 {
			t.Errorf("segment %d has multiplier %d bp, not a whole multiple of 10000. "+
				"Fractional multipliers truncate and need a deliberate decision.",
				i+1, seg.MultBP)
		}
		for _, stake := range []int64{1, 7, 333, 999, 1_000_001, 4_999_999} {
			if (stake*int64(seg.MultBP))%10_000 != 0 {
				t.Errorf("stake %d at %d bp leaves a remainder", stake, seg.MultBP)
			}
		}
	}
}

// When truncation does happen it must match Postgres. Both truncate toward
// zero; every value here is non-negative, so both floor.
func TestTruncationIsTowardZero(t *testing.T) {
	cases := []struct {
		stake int64
		bp    int
		want  int64
	}{
		{333, 15_000, 499}, // 499.5 -> 499
		{1, 15_000, 1},     // 1.5   -> 1
		{1, 5_000, 0},      // 0.5   -> 0
		{7, 33_333, 23},    // 23.33 -> 23
		{99, 12_345, 122},  // 122.2 -> 122
	}
	for _, c := range cases {
		if got := game.Payout(c.stake, c.bp); got != c.want {
			t.Errorf("Payout(%d, %dbp) = %d, want %d", c.stake, c.bp, got, c.want)
		}
	}
}

// ── 3. overflow ─────────────────────────────────────────────────────────────

func TestNoOverflowAtTheConfiguredCeiling(t *testing.T) {
	const maxStake = int64(5_000_000) // KES 50,000, the configured ceiling
	maxBP := game.MaxMultBP()

	product := maxStake * int64(maxBP)
	if product < 0 {
		t.Fatal("stake × bp overflowed int64")
	}
	if got, want := game.Payout(maxStake, maxBP), maxStake*200; got != want {
		t.Errorf("max payout = %d, want %d", got, want)
	}
	t.Logf("worst case %d × %d = %d — int64 headroom %.0f×",
		maxStake, maxBP, product, float64(math.MaxInt64)/float64(product))
}

// ── 4. Go and Postgres must agree, bit for bit ──────────────────────────────

// The schema enforces `payout_cents = stake_cents * multiplier_bp / 10000`.
// If Go ever diverged from Postgres, every spin would fail that CHECK — or a
// subtly different number would be stored. So compute both and compare rather
// than assuming two languages round alike.
func TestGoPayoutMatchesPostgresExactly(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()

	type pair struct {
		stake int64
		bp    int
	}
	var cases []pair
	for _, seg := range game.Wheel {
		for _, stake := range []int64{1, 7, 500, 999, 1_000, 33_333, 999_999, 5_000_000} {
			cases = append(cases, pair{stake, seg.MultBP})
		}
	}
	// Fractional multipliers too, so the comparison covers the rounding path
	// even though the live wheel never exercises it.
	for _, bp := range []int{5_000, 15_000, 12_345, 33_333} {
		for _, stake := range []int64{1, 7, 99, 333, 1_000_001} {
			cases = append(cases, pair{stake, bp})
		}
	}

	for _, c := range cases {
		var pg int64
		if err := pool.QueryRow(ctx,
			`SELECT $1::bigint * $2::integer / 10000`, c.stake, c.bp).Scan(&pg); err != nil {
			t.Fatalf("postgres compute: %v", err)
		}
		if got := game.Payout(c.stake, c.bp); got != pg {
			t.Errorf("DIVERGENCE stake=%d bp=%d: Go=%d Postgres=%d", c.stake, c.bp, got, pg)
		}
	}
	t.Logf("Go and Postgres agree on all %d stake/multiplier combinations", len(cases))
}

// ── 5. the ledger after a real spin ─────────────────────────────────────────

// Proves the full accounting identity for a real-money spin, for EVERY segment
// on the wheel:
//
//	player   += payout - stake
//	referrer += stake × referralBP / 10000
//	bankroll += stake - payout - rake - commission
//	rake     += stake × rakeBP / 10000
//
// and that the total money in the system is unchanged.
func TestRealSpinAccountingIsExactForEverySegment(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()

	const (
		stake      = int64(10_000)    // KES 100
		startReal  = int64(1_000_000) // KES 10,000
		bankroll   = int64(500_000_000)
		rakeBP     = 500
		referralBP = 200
	)

	for i, seg := range game.Wheel {
		index := i + 1
		t.Run(fmt.Sprintf("seg%02d_x%d", index, seg.MultBP/10_000), func(t *testing.T) {
			testutil.Reset(t, pool)
			testutil.FundHouse(t, pool, bankroll)

			referrer := testutil.NewUser(t, pool, nextPhone())
			player := testutil.NewUser(t, pool, nextPhone(),
				testutil.WithReal(startReal), testutil.ReferredBy(referrer))

			svc := game.NewService(pool, game.Economics{
				RTPBP: 9000, RakeBP: rakeBP, ReferralBP: referralBP,
				MinStakeCents: 500, MaxStakeCents: 5_000_000,
			}).WithPicker(forceSegment(index))

			res, err := svc.Spin(ctx, game.Request{
				UserID: player, StakeCents: stake, IsReal: true,
				ClientRef: fmt.Sprintf("acct-%d", index),
			})
			if err != nil {
				t.Fatalf("spin: %v", err)
			}
			if res.SegmentIndex != index {
				t.Fatalf("forced segment %d but got %d", index, res.SegmentIndex)
			}

			wantPayout := stake * int64(seg.MultBP) / 10_000
			wantRake := stake * rakeBP / 10_000
			wantComm := stake * referralBP / 10_000

			// The headline rule.
			if res.PayoutCents != wantPayout {
				t.Errorf("payout = %d, want %d (%d × %dx)",
					res.PayoutCents, wantPayout, stake, seg.MultBP/10_000)
			}
			if res.NetCents != wantPayout-stake {
				t.Errorf("net = %d, want %d", res.NetCents, wantPayout-stake)
			}

			// Player.
			gotPlayer := testutil.Balances(t, pool, player).RealCents
			wantPlayer := startReal - stake + wantPayout
			if gotPlayer != wantPlayer {
				t.Errorf("player balance = %d, want %d", gotPlayer, wantPlayer)
			}
			if res.BalanceCents != wantPlayer {
				t.Errorf("reported balance = %d, want %d", res.BalanceCents, wantPlayer)
			}

			// Referrer.
			if got := testutil.Balances(t, pool, referrer).RealCents; got != wantComm {
				t.Errorf("referrer = %d, want %d (%d bp of %d)",
					got, wantComm, referralBP, stake)
			}

			// House.
			h := testutil.House(t, pool)
			wantBankroll := bankroll + stake - wantPayout - wantRake - wantComm
			if h.BankrollCents != wantBankroll {
				t.Errorf("bankroll = %d, want %d", h.BankrollCents, wantBankroll)
			}
			if h.RakeCents != wantRake {
				t.Errorf("rake = %d, want %d", h.RakeCents, wantRake)
			}

			// CONSERVATION — nothing created, nothing destroyed.
			before := startReal + bankroll
			after := gotPlayer + testutil.Balances(t, pool, referrer).RealCents +
				h.BankrollCents + h.RakeCents
			if before != after {
				t.Errorf("MONEY NOT CONSERVED: %d before, %d after (delta %+d)",
					before, after, after-before)
			}
		})
	}
}

// A demo spin uses the same wheel and the same arithmetic, but must not touch
// real money anywhere in the system.
func TestDemoSpinTouchesNoRealMoney(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	testutil.Reset(t, pool)
	testutil.FundHouse(t, pool, 500_000_000)

	referrer := testutil.NewUser(t, pool, nextPhone())
	player := testutil.NewUser(t, pool, nextPhone(),
		testutil.WithReal(7_777), testutil.WithDemo(500_000), testutil.ReferredBy(referrer))

	svc := game.NewService(pool, game.Economics{
		RTPBP: 9000, RakeBP: 500, ReferralBP: 200,
		MinStakeCents: 500, MaxStakeCents: 5_000_000,
	}).WithPicker(forceSegment(fiveXSegment(t)))

	res, err := svc.Spin(ctx, game.Request{
		UserID: player, StakeCents: 10_000, IsReal: false, ClientRef: "demo-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.PayoutCents != 50_000 {
		t.Errorf("demo payout = %d, want 50000 — demo uses the SAME maths", res.PayoutCents)
	}

	b := testutil.Balances(t, pool, player)
	if b.DemoCents != 500_000-10_000+50_000 {
		t.Errorf("demo balance = %d, want 540000", b.DemoCents)
	}
	if b.RealCents != 7_777 {
		t.Errorf("real balance = %d, want 7777 untouched by a demo spin", b.RealCents)
	}
	if got := testutil.Balances(t, pool, referrer).RealCents; got != 0 {
		t.Errorf("referrer earned %d on a DEMO spin, want 0", got)
	}
	if h := testutil.House(t, pool); h.BankrollCents != 500_000_000 || h.RakeCents != 0 {
		t.Errorf("house moved on a demo spin: bankroll=%d rake=%d", h.BankrollCents, h.RakeCents)
	}
}

// An unreferred player pays commission to nobody.
//
// The reference implementation fell back to user id 1, so the owner silently
// collected a cut of every unreferred player's turnover.
func TestUnreferredPlayerPaysNoCommission(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	testutil.Reset(t, pool)
	testutil.FundHouse(t, pool, 500_000_000)

	player := testutil.NewUser(t, pool, nextPhone(), testutil.WithReal(1_000_000))
	svc := game.NewService(pool, game.Economics{
		RTPBP: 9000, RakeBP: 500, ReferralBP: 200,
		MinStakeCents: 500, MaxStakeCents: 5_000_000,
	}).WithPicker(forceSegment(losingSegment(t)))

	if _, err := svc.Spin(ctx, game.Request{
		UserID: player, StakeCents: 10_000, IsReal: true, ClientRef: "noref",
	}); err != nil {
		t.Fatal(err)
	}

	var commissions int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_cents),0) FROM transactions WHERE kind='referral'`).
		Scan(&commissions); err != nil {
		t.Fatal(err)
	}
	if commissions != 0 {
		t.Errorf("%d cents of commission paid for an unreferred player, want 0", commissions)
	}
	// With no referrer the house keeps that 2%.
	h := testutil.House(t, pool)
	if want := int64(500_000_000) + 10_000 - 500; h.BankrollCents != want {
		t.Errorf("bankroll = %d, want %d", h.BankrollCents, want)
	}
}

// ── 6. the long run ─────────────────────────────────────────────────────────

// Over many real spins the house must move by exactly turnover − payouts, with
// rake exactly rakeBP of turnover. This is the M5 acceptance criterion, run
// against real spins through the real transaction rather than a simulation.
func TestBankrollGrowthIsExactOverManySpins(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	pool := testutil.DB(t)
	ctx := context.Background()
	testutil.Reset(t, pool)

	const (
		start  = int64(1_000_000_000)
		stake  = int64(10_000)
		spins  = 400
		rakeBP = 500
	)
	testutil.FundHouse(t, pool, start)
	player := testutil.NewUser(t, pool, nextPhone(), testutil.WithReal(500_000_000))

	// No referrer, so the whole edge stays with the house and the identity is
	// bankroll_delta = turnover - payouts - rake.
	svc := game.NewService(pool, game.Economics{
		RTPBP: 9000, RakeBP: rakeBP, ReferralBP: 200,
		MinStakeCents: 500, MaxStakeCents: 5_000_000,
	})

	var turnover, payouts int64
	for i := 0; i < spins; i++ {
		res, err := svc.Spin(ctx, game.Request{
			UserID: player, StakeCents: stake, IsReal: true,
			ClientRef: fmt.Sprintf("long-%d", i),
		})
		if err != nil {
			t.Fatalf("spin %d: %v", i, err)
		}
		turnover += res.StakeCents
		payouts += res.PayoutCents
		// The per-spin identity, checked every single time.
		if res.PayoutCents != res.StakeCents*int64(res.MultiplierBP)/10_000 {
			t.Fatalf("spin %d violated payout = stake × multiplier", i)
		}
	}

	h := testutil.House(t, pool)
	wantRake := turnover * rakeBP / 10_000
	wantBankroll := start + turnover - payouts - wantRake

	if h.RakeCents != wantRake {
		t.Errorf("rake = %d, want %d (exactly %d bp of %d turnover)",
			h.RakeCents, wantRake, rakeBP, turnover)
	}
	if h.BankrollCents != wantBankroll {
		t.Errorf("bankroll = %d, want %d — off by %+d",
			h.BankrollCents, wantBankroll, h.BankrollCents-wantBankroll)
	}

	t.Logf("%d spins: turnover %d, payouts %d, realised RTP %.0f bp (configured 9000), rake %d",
		spins, turnover, payouts, float64(payouts)/float64(turnover)*10_000, h.RakeCents)
}

// ── helpers ─────────────────────────────────────────────────────────────────

// forceSegment returns a Picker that always lands on the given 1-indexed
// segment, by feeding Pick the exact cumulative weight offset it decodes.
// Production always uses crypto/rand.
func forceSegment(index int) game.Picker {
	var cum uint32
	for i, s := range game.Wheel {
		if i+1 == index {
			break
		}
		cum += uint32(s.Weight)
	}
	return game.Picker{Rand: repeatReader{cum}}
}

func fiveXSegment(t *testing.T) int {
	t.Helper()
	for i, s := range game.Wheel {
		if s.MultBP == 50_000 {
			return i + 1
		}
	}
	t.Fatal("no 5x segment on the wheel")
	return 0
}

func losingSegment(t *testing.T) int {
	t.Helper()
	for i, s := range game.Wheel {
		if s.MultBP == 0 {
			return i + 1
		}
	}
	t.Fatal("no losing segment on the wheel")
	return 0
}

// repeatReader yields the same big-endian uint32 on every read, so a Picker
// built on it always draws the same segment.
type repeatReader struct{ v uint32 }

func (r repeatReader) Read(p []byte) (int, error) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], r.v)
	n := copy(p, b[:])
	if n < len(p) {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

var phoneSeq int64 = 254_700_000_000

func nextPhone() string {
	phoneSeq++
	return fmt.Sprint(phoneSeq)
}

// ── 7. overflow guards ──────────────────────────────────────────────────────

// AUDIT FINDING: MaxExposure used to compute `stake * 2_000_000 / 10000` with no
// guard. Above ~4.6e12 cents that wraps NEGATIVE, and a negative exposure makes
// the admission check `bankroll < exposure` false — so a stake no bankroll could
// ever cover would be admitted and paid. Only reachable through a misplaced zero
// in MAX_STAKE_CENTS, but the defence has to be arithmetic, not optimism.
func TestMaxExposureFailsClosedInsteadOfOverflowing(t *testing.T) {
	safe := game.MaxSafeStake()
	t.Logf("safe stake ceiling: %d cents (KES %.0f billion)", safe, float64(safe)/100/1e9)

	// Inside the safe range the answer is exact.
	for _, stake := range []int64{500, 5_000_000, 1_000_000_000, safe} {
		got := game.MaxExposure(stake)
		if got < 0 {
			t.Errorf("MaxExposure(%d) = %d, negative", stake, got)
		}
		if want := stake * 200; stake <= safe && got != want && got != math.MaxInt64 {
			t.Errorf("MaxExposure(%d) = %d, want %d", stake, got, want)
		}
	}

	// Beyond it, fail CLOSED: an exposure no finite bankroll can cover.
	for _, stake := range []int64{safe + 1, 4_700_000_000_000, math.MaxInt64} {
		got := game.MaxExposure(stake)
		if got != math.MaxInt64 {
			t.Errorf("MaxExposure(%d) = %d, want MaxInt64 so every bankroll vetoes it", stake, got)
		}
		// The property that actually matters: admission control rejects it.
		if !(int64(1_000_000_000_000) < got) {
			t.Errorf("a bankroll of 1e12 would ADMIT stake %d", stake)
		}
	}
}

func TestMaxAffordableStakeNeverGoesNegative(t *testing.T) {
	for _, bankroll := range []int64{
		0, -1, 500_000_000, 1_000_000_000_000,
		900_000_000_000_000, 1_000_000_000_000_000, math.MaxInt64,
	} {
		got := game.MaxAffordableStake(bankroll)
		if got < 0 {
			t.Errorf("MaxAffordableStake(%d) = %d, negative", bankroll, got)
		}
		// And it must never claim more than the bankroll could actually pay.
		if got > 0 && game.MaxExposure(got) > bankroll && bankroll > 0 {
			t.Errorf("MaxAffordableStake(%d) = %d but its exposure %d exceeds the bankroll",
				bankroll, got, game.MaxExposure(got))
		}
	}
}

func TestValidateStakeCeilingRefusesAnUnsafeConfig(t *testing.T) {
	if err := game.ValidateStakeCeiling(5_000_000); err != nil {
		t.Errorf("the shipped ceiling was rejected: %v", err)
	}
	for _, bad := range []int64{0, -1, game.MaxSafeStake() + 1, math.MaxInt64} {
		if err := game.ValidateStakeCeiling(bad); err == nil {
			t.Errorf("ValidateStakeCeiling(%d) accepted an unsafe ceiling", bad)
		}
	}
}

// ── 8. truncation across many spins ─────────────────────────────────────────

// Rake and commission truncate PER SPIN, so the total is the sum of truncated
// values, not the truncation of the total:
//
//	SUM floor(s_i * r / 10000)  !=  floor(SUM s_i * r / 10000)
//
// Every cent lost to truncation stays in the bankroll, so the books still
// balance exactly — but the difference is real and anyone reconciling rake
// against turnover needs to expect it. This test pins both facts.
func TestOddStakesTruncatePerSpinAndStillBalance(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	testutil.Reset(t, pool)

	const (
		start      = int64(1_000_000_000)
		rakeBP     = 500
		referralBP = 200
	)
	testutil.FundHouse(t, pool, start)
	referrer := testutil.NewUser(t, pool, nextPhone())
	player := testutil.NewUser(t, pool, nextPhone(),
		testutil.WithReal(10_000_000), testutil.ReferredBy(referrer))

	svc := game.NewService(pool, game.Economics{
		RTPBP: 9000, RakeBP: rakeBP, ReferralBP: referralBP,
		MinStakeCents: 500, MaxStakeCents: 5_000_000,
	})

	// Deliberately awkward stakes: none of these divide 10000 evenly.
	stakes := []int64{501, 733, 999, 1_111, 2_507, 3_333, 4_999, 7_777}

	var turnover, payouts, wantRake, wantComm int64
	for i, stake := range stakes {
		res, err := svc.Spin(ctx, game.Request{
			UserID: player, StakeCents: stake, IsReal: true,
			ClientRef: fmt.Sprintf("odd-%d", i),
		})
		if err != nil {
			t.Fatalf("spin %d (stake %d): %v", i, stake, err)
		}
		// The headline identity holds for awkward stakes too.
		if want := stake * int64(res.MultiplierBP) / 10_000; res.PayoutCents != want {
			t.Errorf("stake %d at %d bp: payout %d, want %d",
				stake, res.MultiplierBP, res.PayoutCents, want)
		}
		turnover += stake
		payouts += res.PayoutCents
		wantRake += stake * rakeBP / 10_000     // truncated PER SPIN
		wantComm += stake * referralBP / 10_000 // truncated PER SPIN
	}

	h := testutil.House(t, pool)
	gotComm := testutil.Balances(t, pool, referrer).RealCents

	if h.RakeCents != wantRake {
		t.Errorf("rake = %d, want %d (sum of per-spin truncations)", h.RakeCents, wantRake)
	}
	if gotComm != wantComm {
		t.Errorf("commission = %d, want %d", gotComm, wantComm)
	}
	if want := start + turnover - payouts - wantRake - wantComm; h.BankrollCents != want {
		t.Errorf("bankroll = %d, want %d", h.BankrollCents, want)
	}

	// The truncation gap is real, and it stays in the bankroll — it is not
	// money going missing.
	bulkRake := turnover * rakeBP / 10_000
	t.Logf("turnover %d: per-spin rake %d vs bulk %d (gap %d cents retained in the bankroll)",
		turnover, wantRake, bulkRake, bulkRake-wantRake)
	if wantRake > bulkRake {
		t.Errorf("per-spin rake %d exceeds the bulk figure %d — truncation went the wrong way",
			wantRake, bulkRake)
	}
}
