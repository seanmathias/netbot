package cmd

import (
	"strings"
	"testing"
	"time"
)

// ── pingTarget.record ─────────────────────────────────────────────────────────

func TestPingTargetRecord(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 5}

	pt.record(true, 10*time.Millisecond)
	pt.record(true, 20*time.Millisecond)
	pt.record(false, 0)

	if pt.sent != 3 {
		t.Errorf("sent = %d, want 3", pt.sent)
	}
	if pt.lost != 1 {
		t.Errorf("lost = %d, want 1", pt.lost)
	}
	if pt.recvd != 2 {
		t.Errorf("recvd = %d, want 2", pt.recvd)
	}
	if pt.lastRTT != 20*time.Millisecond {
		t.Errorf("lastRTT = %v, want 20ms (most recent successful reply, not the first)", pt.lastRTT)
	}
}

func TestPingTargetResultsCappedAtNumSlots(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 3}
	for i := 0; i < 10; i++ {
		pt.record(true, time.Millisecond)
	}
	if len(pt.results) != 3 {
		t.Errorf("results len = %d, want 3 (capped at numSlots)", len(pt.results))
	}
	if pt.sent != 10 {
		t.Errorf("sent = %d, want 10 (all pings counted regardless of cap)", pt.sent)
	}
}

func TestPingTargetAvgRTT(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 10}

	if pt.avgRTT() != 0 {
		t.Errorf("avgRTT() = %v, want 0 before any replies", pt.avgRTT())
	}

	pt.record(true, 10*time.Millisecond)
	pt.record(true, 30*time.Millisecond)
	pt.record(false, 0) // loss must not affect average

	if got, want := pt.avgRTT(), 20*time.Millisecond; got != want {
		t.Errorf("avgRTT() = %v, want %v", got, want)
	}
}

func TestPingTargetStats(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 10}

	zero := pt.stats()
	if zero.hasRecv {
		t.Error("hasRecv = true before any replies, want false")
	}
	if zero.sent != 0 || zero.lost != 0 {
		t.Errorf("stats before any pings = %+v, want all zero", zero)
	}

	pt.record(true, 10*time.Millisecond)
	pt.record(true, 30*time.Millisecond)
	pt.record(false, 0)

	snap := pt.stats()
	if snap.sent != 3 {
		t.Errorf("sent = %d, want 3", snap.sent)
	}
	if snap.lost != 1 {
		t.Errorf("lost = %d, want 1", snap.lost)
	}
	if snap.lastRTT != 30*time.Millisecond {
		t.Errorf("lastRTT = %v, want 30ms", snap.lastRTT)
	}
	if snap.avgRTT != 20*time.Millisecond {
		t.Errorf("avgRTT = %v, want 20ms", snap.avgRTT)
	}
	if !snap.hasRecv {
		t.Error("hasRecv = false after a successful reply, want true")
	}
}

// ── timelineCells: basic shape ──────────────────────────────────────────────

func TestTimelineCellsEmptyBeforeFirstResult(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 10}
	cells := pt.timelineCells(10)
	if len(cells) != 10 {
		t.Fatalf("len(cells) = %d, want 10", len(cells))
	}
	for i, c := range cells {
		if c.hasData {
			t.Errorf("cell[%d].hasData = true before any pings, want false", i)
		}
	}
}

func TestTimelineCellsNilForNonPositiveWidth(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 10}
	if got := pt.timelineCells(0); got != nil {
		t.Errorf("timelineCells(0) = %v, want nil", got)
	}
	if got := pt.timelineCells(-5); got != nil {
		t.Errorf("timelineCells(-5) = %v, want nil", got)
	}
}

func TestTimelineCellsRightAligned(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 10}
	pt.record(true, time.Millisecond)
	pt.record(false, 0)
	pt.record(true, time.Millisecond)

	cells := pt.timelineCells(10)
	if len(cells) != 10 {
		t.Fatalf("len(cells) = %d, want 10", len(cells))
	}
	for i := 0; i < 7; i++ {
		if cells[i].hasData {
			t.Errorf("cell[%d].hasData = true, want false (right-aligned padding)", i)
		}
	}
}

func TestTimelineCellsClipsToDisplayWidth(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 90}
	for i := 0; i < 90; i++ {
		pt.record(i%5 != 0, time.Millisecond)
	}
	cells := pt.timelineCells(30)
	if len(cells) != 30 {
		t.Errorf("len(cells) = %d, want 30", len(cells))
	}
}

// ── timelineCells: loss fraction tracking ───────────────────────────────────

func TestTimelineCellsLossFractionAllSuccess(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 3}
	pt.record(true, 5*time.Millisecond)
	pt.record(true, 5*time.Millisecond)
	pt.record(true, 5*time.Millisecond)

	cells := pt.timelineCells(3)
	for i, c := range cells {
		if !c.hasData || !c.anySuccess {
			t.Fatalf("cell[%d] = %+v, want fully populated", i, c)
		}
		if c.lossFrac != 0 {
			t.Errorf("cell[%d].lossFrac = %v, want 0", i, c.lossFrac)
		}
	}
}

func TestTimelineCellsLossFractionTotalLoss(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 1}
	pt.record(false, 0)

	cells := pt.timelineCells(1)
	c := cells[0]
	if !c.hasData {
		t.Fatal("hasData = false, want true (bucket has a recorded ping)")
	}
	if c.anySuccess {
		t.Error("anySuccess = true, want false (every ping in bucket was lost)")
	}
	if c.lossFrac != 1.0 {
		t.Errorf("lossFrac = %v, want 1.0", c.lossFrac)
	}
}

func TestTimelineCellsLossFractionPartial(t *testing.T) {
	// numSlots=8, displayWidth=1 -> bucketSize=8, so seq 1-4 (all four pings
	// recorded below) land in bucket 0 together.
	pt := &pingTarget{host: "10.0.0.1", numSlots: 8}
	pt.record(true, 10*time.Millisecond)
	pt.record(true, 10*time.Millisecond)
	pt.record(false, 0)
	pt.record(false, 0)

	cells := pt.timelineCells(1)
	c := cells[0]
	if !c.hasData || !c.anySuccess {
		t.Fatalf("cell = %+v, want hasData and anySuccess true", c)
	}
	if c.lossFrac != 0.5 {
		t.Errorf("lossFrac = %v, want 0.5 (2 of 4 lost)", c.lossFrac)
	}
	if c.avgRTT != 10*time.Millisecond {
		t.Errorf("avgRTT = %v, want 10ms (average of the 2 successful replies only)", c.avgRTT)
	}
}

// ── timelineCells: bucket stability across ticks ────────────────────────────

// TestTimelineCellsFrozenBucketStaysStable is the direct regression test for
// the bug where past (already-rendered) timeline cells changed appearance as
// the window slid forward. A bucket that has already been superseded by a
// newer "current" bucket must report identical aggregates forever after,
// even as many more pings arrive in later buckets.
func TestTimelineCellsFrozenBucketStaysStable(t *testing.T) {
	const numSlots = 30 // large enough that nothing is evicted during this test
	pt := &pingTarget{host: "10.0.0.1", numSlots: numSlots}

	// Phase 1: 10 pings at 10ms. With displayWidth=3 below, bucketSize=10,
	// so seq 1-9 fill bucket 0 completely and seq 10 starts bucket 1 (the
	// "current" bucket at this point — bucket 0 is already frozen).
	for i := 0; i < 10; i++ {
		pt.record(true, 10*time.Millisecond)
	}
	cellsA := pt.timelineCells(3)

	// Phase 2: 10 more pings at a very different RTT. This completes
	// bucket 1 (seq 10-19) and starts bucket 2 (seq 20) as the new current
	// bucket. Bucket 0 should not be touched by any of this.
	for i := 0; i < 10; i++ {
		pt.record(true, 500*time.Millisecond)
	}
	cellsB := pt.timelineCells(3)

	// Bucket 0 was already frozen at the time cellsA was captured (it was
	// not the current bucket even then), so its position in cellsA (index 1)
	// must exactly match its position in cellsB (index 0) after the window
	// has slid forward by one bucket.
	if cellsA[1] != cellsB[0] {
		t.Errorf("frozen bucket changed across ticks: before=%+v after=%+v", cellsA[1], cellsB[0])
	}

	if !cellsB[0].hasData || !cellsB[0].anySuccess {
		t.Fatal("expected frozen bucket to have data")
	}
	if cellsB[0].avgRTT != 10*time.Millisecond {
		t.Errorf("frozen bucket avgRTT = %v, want 10ms (phase-2 pings must not have leaked in)", cellsB[0].avgRTT)
	}
}

// TestTimelineCellsCurrentBucketUpdatesUntilItRolls confirms the *current*
// (rightmost, still-filling) bucket is allowed to change its average as more
// pings land in it — only buckets that have been superseded must freeze.
func TestTimelineCellsCurrentBucketUpdatesUntilItRolls(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 30}

	pt.record(true, 10*time.Millisecond) // seq1 — starts bucket 0 (bucketSize=10 @ displayWidth=3, total=30)
	before := pt.timelineCells(3)

	pt.record(true, 30*time.Millisecond) // seq2 — still bucket 0, same (current) bucket
	after := pt.timelineCells(3)

	lastBefore := before[len(before)-1]
	lastAfter := after[len(after)-1]

	if lastBefore.avgRTT == lastAfter.avgRTT {
		t.Error("expected the current bucket's average to change as a second ping landed in it")
	}
	wantAvg := (10*time.Millisecond + 30*time.Millisecond) / 2
	if lastAfter.avgRTT != wantAvg {
		t.Errorf("current bucket avgRTT = %v, want %v", lastAfter.avgRTT, wantAvg)
	}
}

// TestTimelineCellsCoversFullRangeWhenNarrow verifies a long --range (e.g.
// 300s/600s) with a narrow terminal compresses the *entire* requested range
// into the available cells, rather than silently truncating to only the
// most recent N pings.
func TestTimelineCellsCoversFullRangeWhenNarrow(t *testing.T) {
	const numSlots = 600 // simulates --range 600s --interval 1s
	pt := &pingTarget{host: "10.0.0.1", numSlots: numSlots}

	// Oldest 300 pings succeed, most recent 300 are lost.
	for i := 0; i < 300; i++ {
		pt.record(true, 5*time.Millisecond)
	}
	for i := 0; i < 300; i++ {
		pt.record(false, 0)
	}

	const displayWidth = 60 // narrow terminal — far fewer cells than numSlots
	cells := pt.timelineCells(displayWidth)
	if len(cells) != displayWidth {
		t.Fatalf("len(cells) = %d, want %d", len(cells), displayWidth)
	}

	firstHalfHasSuccess := false
	for i := 0; i < displayWidth/2; i++ {
		if cells[i].anySuccess {
			firstHalfHasSuccess = true
			break
		}
	}
	if !firstHalfHasSuccess {
		t.Error("expected successful cells in the first half of the timeline (older successful pings); " +
			"got none — timeline is likely only showing the most recent pings instead of the full range")
	}

	secondHalfAllLost := true
	for i := displayWidth / 2; i < displayWidth; i++ {
		if cells[i].anySuccess {
			secondHalfAllLost = false
			break
		}
	}
	if !secondHalfAllLost {
		t.Error("expected the second half of the timeline (most recent, all-lost pings) to have no successes")
	}
}

func TestTimelineCellsOneToOneWhenWideEnough(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 10}
	for i := 0; i < 10; i++ {
		pt.record(i%2 == 0, 5*time.Millisecond) // alternating success/loss
	}

	cells := pt.timelineCells(10)
	if len(cells) != 10 {
		t.Fatalf("len(cells) = %d, want 10", len(cells))
	}
	for i, c := range cells {
		wantSuccess := i%2 == 0
		if c.anySuccess != wantSuccess {
			t.Errorf("cell[%d].anySuccess = %v, want %v", i, c.anySuccess, wantSuccess)
		}
	}
}

func TestTimelineCellsBucketAverageReflectsAllSamples(t *testing.T) {
	pt := &pingTarget{host: "10.0.0.1", numSlots: 4}
	pt.record(true, 1*time.Millisecond)
	pt.record(true, 199*time.Millisecond)
	// Average of this bucket (if binned together) = 100ms.

	cells := pt.timelineCells(1) // force both pings into a single cell
	if !cells[0].anySuccess {
		t.Fatal("expected the single cell to have a successful average")
	}
	want := 100 * time.Millisecond
	if cells[0].avgRTT != want {
		t.Errorf("avgRTT = %v, want %v (bucket average, not just one ping)", cells[0].avgRTT, want)
	}
}

// ── severityHex ───────────────────────────────────────────────────────────────

func TestSeverityHex(t *testing.T) {
	tests := []struct {
		name string
		rtt  time.Duration
		want string
	}{
		{"well under green threshold", 1 * time.Millisecond, severityGreenHex},
		{"just under green threshold", pingRTTGreenMax - time.Microsecond, severityGreenHex},
		{"at green threshold rolls to yellow", pingRTTGreenMax, severityYellowHex},
		{"mid yellow range", 40 * time.Millisecond, severityYellowHex},
		{"at yellow threshold rolls to orange", pingRTTYellowMax, severityOrangeHex},
		{"mid orange range", 90 * time.Millisecond, severityOrangeHex},
		{"at orange threshold rolls to red", pingRTTOrangeMax, severityRedHex},
		{"well above orange threshold", 500 * time.Millisecond, severityRedHex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := severityHex(tt.rtt); got != tt.want {
				t.Errorf("severityHex(%v) = %q, want %q", tt.rtt, got, tt.want)
			}
		})
	}
}

// ── lerpHex ───────────────────────────────────────────────────────────────────

func TestLerpHex(t *testing.T) {
	tests := []struct {
		name   string
		c1, c2 string
		frac   float64
		want   string
	}{
		{"t=0 returns first colour exactly", "#2ECC71", "#4A4A4A", 0, "#2ecc71"},
		{"t=1 returns second colour exactly", "#2ECC71", "#4A4A4A", 1, "#4a4a4a"},
		{"t=0.5 midpoint", "#000000", "#FFFFFF", 0.5, "#7f7f7f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lerpHex(tt.c1, tt.c2, tt.frac)
			if got != tt.want {
				t.Errorf("lerpHex(%s, %s, %v) = %q, want %q", tt.c1, tt.c2, tt.frac, got, tt.want)
			}
		})
	}
}

// ── timelineStyle / timelineGlyph ───────────────────────────────────────────

func TestTimelineGlyphBlankWhenNoData(t *testing.T) {
	c := timelineCell{hasData: false}
	if got := timelineGlyph(c); got != " " {
		t.Errorf("timelineGlyph(no data) = %q, want a blank space", got)
	}
}

func TestTimelineGlyphBlankWhenTotalLoss(t *testing.T) {
	c := timelineCell{hasData: true, anySuccess: false, lossFrac: 1.0}
	if got := timelineGlyph(c); got != " " {
		t.Errorf("timelineGlyph(total loss) = %q, want a blank space", got)
	}
}

func TestTimelineGlyphRendersWhenAnySuccess(t *testing.T) {
	c := timelineCell{hasData: true, anySuccess: true, avgRTT: 5 * time.Millisecond, lossFrac: 0}
	got := timelineGlyph(c)
	if strings.TrimSpace(stripANSI(got)) == "" {
		t.Errorf("timelineGlyph(success) = %q, want a non-blank glyph", got)
	}
}

func TestTimelineGlyphHeightScalesWithRTT(t *testing.T) {
	low := timelineGlyph(timelineCell{hasData: true, anySuccess: true, avgRTT: 1 * time.Millisecond})
	high := timelineGlyph(timelineCell{hasData: true, anySuccess: true, avgRTT: 500 * time.Millisecond})

	lowGlyph := []rune(stripANSI(low))[0]
	highGlyph := []rune(stripANSI(high))[0]

	if lowGlyph == highGlyph {
		t.Error("expected different glyph heights for very different RTTs")
	}
	// 1ms should map to the lowest level, 500ms (well above sparkMaxScale) to the highest.
	if lowGlyph != sparkLevels[0] {
		t.Errorf("low RTT glyph = %q, want lowest level %q", lowGlyph, sparkLevels[0])
	}
	if highGlyph != sparkLevels[len(sparkLevels)-1] {
		t.Errorf("high RTT glyph = %q, want highest level %q", highGlyph, sparkLevels[len(sparkLevels)-1])
	}
}

func TestBlendedHexFadesWithLossFraction(t *testing.T) {
	reliable := blendedHex(5*time.Millisecond, 0)
	lossy := blendedHex(5*time.Millisecond, 0.7)

	if reliable == lossy {
		t.Error("expected a lossy bucket to produce a different (faded) colour than a fully reliable one")
	}
	// Sanity: reliable should be the unfaded base severity colour. lerpHex
	// always formats its output in lowercase via %02x, so compare against
	// the lowercased constant rather than severityGreenHex's literal case.
	if reliable != strings.ToLower(severityGreenHex) {
		t.Errorf("blendedHex(5ms, 0) = %q, want unfaded base colour %q", reliable, strings.ToLower(severityGreenHex))
	}
}

func TestBlendedHexCapsFadeBelowFullGray(t *testing.T) {
	// Even at 100% loss fraction, the colour should stop fading at maxFade
	// rather than reaching pure gray, so a badly-lossy bucket still reads
	// as "this severity colour, degraded" rather than becoming
	// indistinguishable from having no data at all.
	atMax := blendedHex(5*time.Millisecond, maxFade)
	beyondMax := blendedHex(5*time.Millisecond, 1.0)

	if atMax != beyondMax {
		t.Errorf("blendedHex should clamp at maxFade: got %q at maxFade and %q at 1.0, want equal", atMax, beyondMax)
	}
	if beyondMax == fadeGrayHex {
		t.Errorf("blendedHex(_, 1.0) = %q, should not reach pure gray due to maxFade cap", beyondMax)
	}
}

func TestSparkGlyphForClampsAtBounds(t *testing.T) {
	if got := sparkGlyphFor(-5 * time.Millisecond); got != sparkLevels[0] {
		t.Errorf("sparkGlyphFor(negative) = %q, want lowest level %q", got, sparkLevels[0])
	}
	if got := sparkGlyphFor(sparkMaxScale * 10); got != sparkLevels[len(sparkLevels)-1] {
		t.Errorf("sparkGlyphFor(far above scale) = %q, want highest level %q", got, sparkLevels[len(sparkLevels)-1])
	}
}

// ── pingFormatRTT ─────────────────────────────────────────────────────────────

func TestPingFormatRTT(t *testing.T) {
	tests := []struct {
		d     time.Duration
		valid bool
		want  string
	}{
		{0, false, "—"},
		{500 * time.Microsecond, true, "500µs"},
		{1500 * time.Microsecond, true, "1.50ms"},
		{14300 * time.Microsecond, true, "14.30ms"},
		{2 * time.Second, true, "2.00s"},
	}
	for _, tt := range tests {
		got := pingFormatRTT(tt.d, tt.valid)
		if got != tt.want {
			t.Errorf("pingFormatRTT(%v, %v) = %q, want %q", tt.d, tt.valid, got, tt.want)
		}
	}
}

// ── pingFormatDuration ────────────────────────────────────────────────────────

func TestPingFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{60 * time.Second, "1m"},
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		// 90s is past the 60s cutoff into the minutes branch, where it
		// rounds to the nearest whole minute (1.5m rounds up to 2m).
		{90 * time.Second, "2m"},
	}
	for _, tt := range tests {
		if got := pingFormatDuration(tt.d); got != tt.want {
			t.Errorf("pingFormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// ── padding helpers ───────────────────────────────────────────────────────────

func TestPingPadHelpers(t *testing.T) {
	if got := pingPadRight("abc", 6); got != "abc   " {
		t.Errorf("pingPadRight = %q, want %q", got, "abc   ")
	}
	if got := pingPadLeft("abc", 6); got != "   abc" {
		t.Errorf("pingPadLeft = %q, want %q", got, "   abc")
	}
	if got := pingPadCenter("ab", 6); got != "  ab  " {
		t.Errorf("pingPadCenter = %q, want %q", got, "  ab  ")
	}
	// Input wider than width should be returned unchanged.
	if got := pingPadRight("toolong", 3); got != "toolong" {
		t.Errorf("pingPadRight wider than width = %q, want unchanged", got)
	}
}

// ── flag validation ───────────────────────────────────────────────────────────

func TestPingIntervalMinimum(t *testing.T) {
	pingFlags.interval = 100 * time.Millisecond
	err := runPing(pingCmd, []string{"127.0.0.1"})
	if err == nil {
		t.Error("expected error for interval < 250ms, got nil")
	}
	if !strings.Contains(err.Error(), "250ms") {
		t.Errorf("error = %q, want mention of 250ms", err.Error())
	}
	pingFlags.interval = time.Second // reset
}

func TestPingRequiresAtLeastOneTarget(t *testing.T) {
	if err := pingCmd.Args(pingCmd, []string{}); err == nil {
		t.Error("expected error with no targets, got nil")
	}
}

// ── privilege warning ────────────────────────────────────────────────────────

func TestPrivilegeWarningMessageContent(t *testing.T) {
	msg := privilegeWarningMessage()
	for _, want := range []string{"root", "setcap", "cap_net_raw", "--privileged=false"} {
		if !strings.Contains(msg, want) {
			t.Errorf("privilegeWarningMessage() = %q, want it to mention %q", msg, want)
		}
	}
}

func TestPingModelViewShowsWarningBannerWhenSet(t *testing.T) {
	m := newPingModel(nil, time.Second, 10, "test warning text")
	view := stripANSI(m.View())
	if !strings.Contains(view, "test warning text") {
		t.Errorf("View() did not contain the privilege warning banner: %q", view)
	}
	if !strings.Contains(view, "⚠") {
		t.Error("View() warning banner missing the warning indicator")
	}
}

func TestPingModelViewOmitsWarningBannerWhenEmpty(t *testing.T) {
	m := newPingModel(nil, time.Second, 10, "")
	view := stripANSI(m.View())
	if strings.Contains(view, "⚠") {
		t.Error("View() rendered a warning banner when no privilege warning was set")
	}
}

// ── ANSI helper ──────────────────────────────────────────────────────────────

// stripANSI removes ANSI escape sequences so tests can check raw text content.
func stripANSI(s string) string {
	var out strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			i += 2
			for i < len(runes) && runes[i] != 'm' {
				i++
			}
			i++
		} else {
			out.WriteRune(runes[i])
			i++
		}
	}
	return out.String()
}
