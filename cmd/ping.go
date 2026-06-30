package cmd

import (
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	probing "github.com/prometheus-community/pro-bing"
	"github.com/spf13/cobra"
)

// ── Flags ─────────────────────────────────────────────────────────────────────

var pingFlags struct {
	interval   time.Duration
	timeRange  time.Duration
	privileged bool
}

var pingCmd = &cobra.Command{
	Use:   "ping <target> [target ...]",
	Short: "Continuous ICMP ping with a live results table",
	Long: `Sends continuous ICMP pings to one or more targets and displays a live
table showing a reachability timeline, packet counts, and RTT statistics.

Each cell in the timeline represents one bucket of time (one ping, or
several binned together when --range doesn't fit one-cell-per-ping in the
terminal) and encodes three signals at once:

  Height  RTT magnitude (▁▂▃▄▅▆▇█) — taller means higher latency
  Color   RTT severity (green → yellow → orange → red as latency increases)
  Fade    packet loss density within that cell — vivid means every ping in
          the cell got a reply, washed-out gray means some were lost,
          blank means every ping in the cell was lost (no RTT data at all)

This means a cell can show "high latency but reliable" (tall, vivid red)
distinctly from "same latency, but partially lossy" (tall, faded red) —
information a flat coloured marker alone can't convey.

ICMP requires elevated privileges. Either run as root or grant the raw socket
capability to the binary once:

  sudo setcap cap_net_raw+ep ./netbot

Alternatively, use --privileged=false to fall back to UDP-based ICMP which
works without root on most Linux systems.`,
	Example: `  netbot ping 192.168.1.1
  netbot ping 8.8.8.8 1.1.1.1 192.168.1.1
  netbot ping 8.8.8.8 --interval 500ms --range 60s
  netbot ping 8.8.8.8 --range 300s
  netbot ping 8.8.8.8 --privileged=false`,
	Args:         cobra.MinimumNArgs(1),
	RunE:         runPing,
	SilenceUsage: true,
}

func init() {
	f := pingCmd.Flags()
	f.DurationVarP(&pingFlags.interval, "interval", "i", time.Second,
		"Time between pings per target (minimum 250ms)")
	f.DurationVarP(&pingFlags.timeRange, "range", "r", 90*time.Second,
		"Duration of history shown in the timeline column")
	f.BoolVar(&pingFlags.privileged, "privileged", true,
		"Use raw ICMP sockets (requires root or cap_net_raw). Set false for unprivileged UDP mode")
}

func runPing(cmd *cobra.Command, args []string) error {
	if pingFlags.interval < 250*time.Millisecond {
		return fmt.Errorf("--interval must be at least 250ms")
	}

	numSlots := int(math.Round(pingFlags.timeRange.Seconds() / pingFlags.interval.Seconds()))
	if numSlots < 1 {
		numSlots = 1
	}

	targets := make([]*pingTarget, len(args))
	for i, host := range args {
		targets[i] = &pingTarget{host: host, numSlots: numSlots}
	}

	// Probe for raw ICMP socket access before launching the TUI. The alt
	// screen (tea.WithAltScreen) clears the terminal entirely, so a warning
	// printed before that point disappears from view the moment the program
	// starts — the person watching the live table would otherwise have no
	// way to tell *why* every ping is failing. The warning is therefore also
	// carried into the model so it renders as a persistent banner in the
	// running view itself, not just before it.
	privilegeWarning := ""
	if pingFlags.privileged && !canUseRawICMP() {
		privilegeWarning = privilegeWarningMessage()
		fmt.Fprintln(os.Stderr, "warning: "+privilegeWarning)
	}

	m := newPingModel(targets, pingFlags.interval, numSlots, privilegeWarning)
	p := tea.NewProgram(m, tea.WithAltScreen())

	for _, t := range targets {
		go pingLoop(t, pingFlags.interval, pingFlags.privileged)
	}

	_, err := p.Run()
	return err
}

// canUseRawICMP reports whether the process can actually open a raw ICMP
// socket right now. This is a real permission probe — not just an euid==0
// check — so it correctly reflects root, a setcap-granted capability, or
// neither, on any platform Go's net package supports raw ICMP on.
func canUseRawICMP() bool {
	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// privilegeWarningMessage is the user-facing text shown both on stderr
// before the TUI starts and as a persistent banner inside the running view,
// so the warning stays visible even though the alt screen clears anything
// printed beforehand.
func privilegeWarningMessage() string {
	return "Running without raw ICMP socket privileges — every ping below will report as lost. " +
		"Run as root, grant the capability once with 'sudo setcap cap_net_raw+ep ./netbot', " +
		"or restart with --privileged=false to use unprivileged UDP-based ICMP."
}

// ── Ping goroutine ────────────────────────────────────────────────────────────
//
// pingLoop writes results directly into t (under t.mu) rather than routing
// every single reply through the Bubble Tea message queue. Routing each
// reply as its own message would cause Bubble Tea to redraw the whole view
// after every reply — with several targets replying at slightly different
// times within the same interval, that produces a visible cluster of
// partial repaints each tick instead of one clean one. The only thing
// driving a redraw is the periodic pingTickMsg in the model, so the table
// repaints exactly once per --interval, fully up to date, with no
// intermediate partial frames.
func pingLoop(t *pingTarget, interval time.Duration, privileged bool) {
	for {
		cycleStart := time.Now()

		pinger, err := probing.NewPinger(t.host)
		if err != nil {
			t.record(false, 0)
		} else {
			pinger.Count = 1
			pinger.Timeout = interval
			pinger.SetPrivileged(privileged)

			runErr := pinger.Run()
			stats := pinger.Statistics()

			if runErr != nil || stats.PacketsRecv == 0 {
				t.record(false, 0)
			} else {
				t.record(true, stats.AvgRtt)
			}
		}

		if sleep := interval - time.Since(cycleStart); sleep > 0 {
			time.Sleep(sleep)
		}
	}
}

// ── Messages ──────────────────────────────────────────────────────────────────

type pingTickMsg time.Time

func pingTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return pingTickMsg(t) })
}

// ── Per-target state ──────────────────────────────────────────────────────────

type pingSlot struct {
	success bool
	rtt     time.Duration
	seq     int // absolute, monotonically increasing ping number for this target
}

// pingTarget is written by exactly one goroutine (its own pingLoop) and read
// by the Bubble Tea render loop on a separate goroutine once per tick. mu
// guards every field below against that concurrent access.
type pingTarget struct {
	host     string
	numSlots int

	mu       sync.RWMutex
	results  []pingSlot
	sent     int
	lost     int
	lastRTT  time.Duration
	totalRTT time.Duration
	recvd    int
}

func (t *pingTarget) record(success bool, rtt time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sent++
	if success {
		t.lastRTT = rtt
		t.totalRTT += rtt
		t.recvd++
	} else {
		t.lost++
	}
	// seq is the absolute, ever-increasing ping number for this target. It is
	// what every bucket boundary in timelineCells() is anchored to, so a
	// ping's bucket assignment never changes once recorded — see
	// timelineCells() for why this matters.
	t.results = append(t.results, pingSlot{success: success, rtt: rtt, seq: t.sent})
	if len(t.results) > t.numSlots {
		t.results = t.results[len(t.results)-t.numSlots:]
	}
}

func (t *pingTarget) avgRTT() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.recvd == 0 {
		return 0
	}
	return t.totalRTT / time.Duration(t.recvd)
}

// pingStats is a point-in-time copy of a target's scalar counters, taken
// under lock, so render code can read several related fields consistently
// without holding the lock across the whole row-rendering call.
type pingStats struct {
	sent    int
	lost    int
	lastRTT time.Duration
	avgRTT  time.Duration
	hasRecv bool
}

func (t *pingTarget) stats() pingStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var avg time.Duration
	if t.recvd > 0 {
		avg = t.totalRTT / time.Duration(t.recvd)
	}
	return pingStats{
		sent:    t.sent,
		lost:    t.lost,
		lastRTT: t.lastRTT,
		avgRTT:  avg,
		hasRecv: t.recvd > 0,
	}
}

// ── Bucket aggregation ───────────────────────────────────────────────────────

// timelineCell is the aggregated state of a single display cell, computed
// before any styling is applied. Exposing this separately from timeline()
// lets tests verify bucket stability and loss-fraction numerically rather
// than by parsing ANSI-wrapped strings.
type timelineCell struct {
	hasData    bool
	anySuccess bool
	avgRTT     time.Duration
	lossFrac   float64 // 0.0 = no loss in this bucket, 1.0 = total loss
}

// timelineCells computes the per-cell bucket aggregates representing the
// full configured time range (t.numSlots pings), right-aligned (most recent
// on the right). When the terminal is too narrow to show one cell per ping,
// multiple consecutive pings are binned into a single cell so the entire
// requested --range is always represented, rather than silently truncating
// to whatever fits.
//
// Bucket boundaries are anchored to each ping's absolute sequence number
// (pingSlot.seq), not to its position relative to "now". This is essential:
// if buckets were instead computed from a ping's offset from the current
// moment, every tick would shift which actual pings fall into which bucket,
// causing already-rendered (past) cells to silently recolour as the window
// slides — which is exactly the bug this anchoring avoids. With seq-based
// buckets, a ping belongs to exactly one bucket for the rest of its life.
// Only the single rightmost (current) bucket is still being filled and can
// change appearance tick to tick; every bucket to its left is permanently
// frozen the moment a newer ping pushes the "current" bucket forward.
//
// A cell with hasData=false renders as a blank gap, as does a cell whose
// bucket has data but every ping in it was lost (anySuccess=false). A cell
// with partial loss (0 < lossFrac < 1) still has anySuccess=true and
// avgRTT reflecting just the successful replies in that bucket.
//
// The returned slice always has length displayWidth (or nil if displayWidth <= 0).
func (t *pingTarget) timelineCells(displayWidth int) []timelineCell {
	if displayWidth <= 0 {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	cells := make([]timelineCell, displayWidth)

	n := len(t.results)
	if n == 0 {
		return cells // all zero-value: hasData=false
	}

	total := t.numSlots
	if total <= 0 {
		total = 1
	}
	bucketSize := total / displayWidth
	if bucketSize < 1 {
		bucketSize = 1
	}

	type bucketAcc struct {
		total    int
		lost     int
		rttSum   time.Duration
		rttCount int
	}
	buckets := make(map[int]*bucketAcc, displayWidth)
	for i := range t.results {
		r := &t.results[i]
		b := r.seq / bucketSize
		acc := buckets[b]
		if acc == nil {
			acc = &bucketAcc{}
			buckets[b] = acc
		}
		acc.total++
		if r.success {
			acc.rttSum += r.rtt
			acc.rttCount++
		} else {
			acc.lost++
		}
	}

	currentBucket := t.results[n-1].seq / bucketSize

	for c := 0; c < displayWidth; c++ {
		b := currentBucket - (displayWidth - 1 - c)
		acc := buckets[b]
		if acc == nil || acc.total == 0 {
			continue // leave zero-value: hasData=false
		}
		cells[c].hasData = true
		cells[c].lossFrac = float64(acc.lost) / float64(acc.total)
		if acc.rttCount > 0 {
			cells[c].anySuccess = true
			cells[c].avgRTT = acc.rttSum / time.Duration(acc.rttCount)
		}
	}
	return cells
}

// ── Sparkline glyph selection ────────────────────────────────────────────────

// sparkLevels are the eight Unicode block-height glyphs used to encode RTT
// magnitude, from lowest (▁) to highest (█).
var sparkLevels = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// sparkMaxScale is the RTT value mapped to the tallest bar (█). Chosen as
// 2x the orange threshold so the gradient has headroom before everything
// pins to full height — tune this if your typical link latency is very
// different from a LAN/WAN baseline.
const sparkMaxScale = pingRTTOrangeMax * 2

// sparkGlyphFor maps an RTT to one of the eight height levels, scaled
// linearly against sparkMaxScale and clamped at both ends.
func sparkGlyphFor(avg time.Duration) rune {
	if avg < 0 {
		avg = 0
	}
	idx := int(float64(avg) / float64(sparkMaxScale) * float64(len(sparkLevels)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sparkLevels) {
		idx = len(sparkLevels) - 1
	}
	return sparkLevels[idx]
}

// ── Colour blending ───────────────────────────────────────────────────────────

// RTT severity thresholds. Latency below pingRTTGreenMax is healthy (green),
// rising through yellow and orange before reaching red at or above
// pingRTTOrangeMax. Tuned for typical LAN/WAN round-trip times; adjust if
// pinging links with very different baseline latency.
const (
	pingRTTGreenMax  = 20 * time.Millisecond
	pingRTTYellowMax = 60 * time.Millisecond
	pingRTTOrangeMax = 120 * time.Millisecond
)

const (
	severityGreenHex  = "#2ECC71"
	severityYellowHex = "#F1C40F"
	severityOrangeHex = "#E67E22"
	severityRedHex    = "#E74C3C"
	fadeGrayHex       = "#4A4A4A" // fade target as loss density increases
)

// maxFade caps how far a colour fades toward gray. Capped below 1.0 so a
// bucket with, say, 90% loss still reads as "this colour, badly degraded"
// rather than becoming indistinguishable from a fully blank/no-data cell.
const maxFade = 0.85

// severityHex returns the base (unfaded) hex colour for a given RTT,
// implementing the green → yellow → orange → red severity gradient.
func severityHex(avg time.Duration) string {
	switch {
	case avg < pingRTTGreenMax:
		return severityGreenHex
	case avg < pingRTTYellowMax:
		return severityYellowHex
	case avg < pingRTTOrangeMax:
		return severityOrangeHex
	default:
		return severityRedHex
	}
}

// lerpHex linearly interpolates between two "#RRGGBB" colours at t ∈ [0,1].
func lerpHex(c1, c2 string, t float64) string {
	var r1, g1, b1, r2, g2, b2 int
	fmt.Sscanf(c1, "#%02x%02x%02x", &r1, &g1, &b1)
	fmt.Sscanf(c2, "#%02x%02x%02x", &r2, &g2, &b2)
	r := int(float64(r1) + (float64(r2)-float64(r1))*t)
	g := int(float64(g1) + (float64(g2)-float64(g1))*t)
	b := int(float64(b1) + (float64(b2)-float64(b1))*t)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// blendedHex computes the final hex colour for a cell: the severity colour
// for its RTT, faded toward gray by its loss fraction. Kept separate from
// timelineStyle so tests can assert on the colour value directly rather
// than on rendered ANSI output, which lipgloss may suppress entirely when
// not attached to a real terminal (e.g. under `go test`).
func blendedHex(avg time.Duration, lossFrac float64) string {
	base := severityHex(avg)
	t := lossFrac
	if t > maxFade {
		t = maxFade
	}
	if t < 0 {
		t = 0
	}
	return lerpHex(base, fadeGrayHex, t)
}

// timelineStyle returns a style whose colour reflects both RTT severity
// (hue) and loss density within the bucket (fade toward gray).
func timelineStyle(avg time.Duration, lossFrac float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(blendedHex(avg, lossFrac)))
}

// timelineGlyph renders a single cell: blank if there's no data or the
// bucket was a total loss, otherwise a height-scaled glyph (sparkGlyphFor)
// coloured by severity and faded by loss density.
func timelineGlyph(c timelineCell) string {
	if !c.hasData || !c.anySuccess {
		return " "
	}
	glyph := sparkGlyphFor(c.avgRTT)
	return timelineStyle(c.avgRTT, c.lossFrac).Render(string(glyph))
}

func (t *pingTarget) timeline(displayWidth int) string {
	cells := t.timelineCells(displayWidth)
	var sb strings.Builder
	for _, c := range cells {
		sb.WriteString(timelineGlyph(c))
	}
	return sb.String()
}

// ── Bubbletea model ───────────────────────────────────────────────────────────

type pingModel struct {
	targets          []*pingTarget
	interval         time.Duration
	numSlots         int
	width            int
	height           int
	privilegeWarning string
}

func newPingModel(targets []*pingTarget, interval time.Duration, numSlots int, privilegeWarning string) pingModel {
	return pingModel{
		targets:          targets,
		interval:         interval,
		numSlots:         numSlots,
		width:            120,
		height:           24,
		privilegeWarning: privilegeWarning,
	}
}

func (m pingModel) Init() tea.Cmd {
	return pingTick(m.interval)
}

func (m pingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pingTickMsg:
		return m, pingTick(m.interval)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pingModel) View() string {
	legend := pingHintStyle.Render("  Height = latency   Color = severity   Fade = loss density   Press q to quit")

	if m.privilegeWarning == "" {
		return renderPingTable(m) + "\n" + legend
	}

	banner := pingWarnStyle.Render("  ⚠ " + m.privilegeWarning)
	return banner + "\n\n" + renderPingTable(m) + "\n" + legend
}

// ── Table rendering ───────────────────────────────────────────────────────────

const (
	pingColSent    = 6
	pingColLost    = 6
	pingColLastRTT = 9
	pingColAvgRTT  = 9
	pingCellPad    = 1
)

func renderPingTable(m pingModel) string {
	// Target column is as wide as the widest host name, with a minimum.
	targetWidth := len("Target")
	for _, t := range m.targets {
		if w := utf8.RuneCountInString(t.host); w > targetWidth {
			targetWidth = w
		}
	}

	// Calculate timeline display width from what remains after fixed columns.
	fixedWidth := 2 + // outer left + right borders
		(targetWidth + 2*pingCellPad) + 1 +
		1 + 1 + 1 + // timeline padding + separator
		(pingColSent + 2*pingCellPad) + 1 +
		(pingColLost + 2*pingCellPad) + 1 +
		(pingColLastRTT + 2*pingCellPad) + 1 +
		(pingColAvgRTT + 2*pingCellPad)

	timelineWidth := m.width - fixedWidth
	if timelineWidth < 10 {
		timelineWidth = 10
	}
	if timelineWidth > m.numSlots {
		timelineWidth = m.numSlots
	}

	timelineHeader := fmt.Sprintf("Last %s", pingFormatDuration(m.interval*time.Duration(m.numSlots)))

	sep := func(l, mid, r, h string) string {
		return pingBorderStyle.Render(
			l +
				strings.Repeat(h, targetWidth+2*pingCellPad) + mid +
				strings.Repeat(h, timelineWidth+2*pingCellPad) + mid +
				strings.Repeat(h, pingColSent+2*pingCellPad) + mid +
				strings.Repeat(h, pingColLost+2*pingCellPad) + mid +
				strings.Repeat(h, pingColLastRTT+2*pingCellPad) + mid +
				strings.Repeat(h, pingColAvgRTT+2*pingCellPad) +
				r)
	}

	pipe := pingBorderStyle.Render("│")

	var sb strings.Builder

	sb.WriteString(sep("╭", "┬", "╮", "─"))
	sb.WriteRune('\n')

	// Header row
	sb.WriteString(pipe)
	sb.WriteString(pingHeaderStyle.Render(pingPadCenter("Target", targetWidth+2*pingCellPad)))
	sb.WriteString(pipe)
	sb.WriteString(pingHeaderStyle.Render(pingPadCenter(timelineHeader, timelineWidth+2*pingCellPad)))
	sb.WriteString(pipe)
	sb.WriteString(pingHeaderStyle.Render(pingPadCenter("Sent", pingColSent+2*pingCellPad)))
	sb.WriteString(pipe)
	sb.WriteString(pingHeaderStyle.Render(pingPadCenter("Lost", pingColLost+2*pingCellPad)))
	sb.WriteString(pipe)
	sb.WriteString(pingHeaderStyle.Render(pingPadCenter("Last RTT", pingColLastRTT+2*pingCellPad)))
	sb.WriteString(pipe)
	sb.WriteString(pingHeaderStyle.Render(pingPadCenter("Avg RTT", pingColAvgRTT+2*pingCellPad)))
	sb.WriteString(pipe)
	sb.WriteRune('\n')

	sb.WriteString(sep("├", "┼", "┤", "─"))
	sb.WriteRune('\n')

	blankRow := func() string {
		return pipe +
			strings.Repeat(" ", targetWidth+2*pingCellPad) + pipe +
			strings.Repeat(" ", timelineWidth+2*pingCellPad) + pipe +
			strings.Repeat(" ", pingColSent+2*pingCellPad) + pipe +
			strings.Repeat(" ", pingColLost+2*pingCellPad) + pipe +
			strings.Repeat(" ", pingColLastRTT+2*pingCellPad) + pipe +
			strings.Repeat(" ", pingColAvgRTT+2*pingCellPad) + pipe
	}

	for i, t := range m.targets {
		if i > 0 {
			// Blank spacer row so a full-height glyph in one row never
			// visually touches the row above it.
			sb.WriteString(blankRow())
			sb.WriteRune('\n')
		}

		snap := t.stats()
		lastRTT := pingFormatRTT(snap.lastRTT, snap.hasRecv)
		avgRTT := pingFormatRTT(snap.avgRTT, snap.hasRecv)
		lostStr := fmt.Sprintf("%d", snap.lost)

		sb.WriteString(pipe)
		sb.WriteString(pingCellStyle.Render(pingPadRight(" "+t.host, targetWidth+2*pingCellPad)))
		sb.WriteString(pipe)
		sb.WriteString(" " + t.timeline(timelineWidth) + " ")
		sb.WriteString(pipe)
		sb.WriteString(pingCellStyle.Render(pingPadLeft(fmt.Sprintf("%d", snap.sent), pingColSent+2*pingCellPad-1) + " "))
		sb.WriteString(pipe)
		lostCell := pingPadLeft(lostStr, pingColLost+2*pingCellPad-1) + " "
		if snap.lost > 0 {
			sb.WriteString(pingLossStyle.Render(lostCell))
		} else {
			sb.WriteString(pingCellStyle.Render(lostCell))
		}
		sb.WriteString(pipe)
		sb.WriteString(pingCellStyle.Render(pingPadLeft(lastRTT, pingColLastRTT+2*pingCellPad-1) + " "))
		sb.WriteString(pipe)
		sb.WriteString(pingCellStyle.Render(pingPadLeft(avgRTT, pingColAvgRTT+2*pingCellPad-1) + " "))
		sb.WriteString(pipe)
		sb.WriteRune('\n')
	}

	sb.WriteString(sep("╰", "┴", "╯", "─"))
	return sb.String()
}

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	pingBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	pingHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	pingCellStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	pingLossStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	pingHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
	pingWarnStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func pingPadRight(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func pingPadLeft(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return strings.Repeat(" ", width-n) + s
}

func pingPadCenter(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	total := width - n
	left := total / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", total-left)
}

func pingFormatRTT(d time.Duration, valid bool) string {
	if !valid {
		return "—"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.0fµs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func pingFormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	return fmt.Sprintf("%.0fm", d.Minutes())
}
