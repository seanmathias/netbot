package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"log/syslog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/scrapli/scrapligo/driver/options"
	"github.com/scrapli/scrapligo/platform"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── Job file schema ───────────────────────────────────────────────────────────

type jobFile struct {
	Credentials credentials `yaml:"credentials"`
	Devices     []jobDevice `yaml:"devices"`
}

type credentials struct {
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
	EnablePassword string `yaml:"enable_password"`
}

type jobDevice struct {
	Host           string `yaml:"host"`
	Name           string `yaml:"name"`
	Platform       string `yaml:"platform"`
	Command        string `yaml:"command"`
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
	EnablePassword string `yaml:"enable_password"`
}

// ── Resolved device ───────────────────────────────────────────────────────────

type device struct {
	Host           string
	Name           string
	Platform       string
	Command        string
	Username       string
	Password       string
	EnablePassword string
}

// ── Command flags ─────────────────────────────────────────────────────────────

var bflags struct {
	hosts          []string
	username       string
	password       string
	enablePassword string
	platform       string
	file           string
	tag            string
	dir            string
	workers        int
	noTimestamp    bool
	quiet          bool
	useSyslog      bool
}

// ── Command definition ────────────────────────────────────────────────────────

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Save the running configuration from one or more network devices",
	Example: `  # Single device
  netbot backup --host 192.168.1.1 -u admin -p secret

  # Multiple devices, enable mode required
  netbot backup --host r1 --host r2 -u admin -p secret --enable-password cisco

  # Via job file with a tag included in filenames
  netbot backup --file job.yaml --tag weekly

  # Custom output directory, no timestamp in filenames
  netbot backup --file job.yaml --dir /backups/network --no-timestamp

  # Run quietly (e.g. from cron), logging to syslog
  netbot backup --file job.yaml --quiet --syslog`,
	RunE:         runBackup,
	SilenceUsage: true,
}

func init() {
	f := backupCmd.Flags()

	// Device targeting
	f.StringVarP(&bflags.file, "file", "f", "", "YAML job file defining devices and credentials")
	f.StringArrayVarP(&bflags.hosts, "host", "H", nil, "Device hostname or IP (repeatable)")
	f.StringVar(&bflags.platform, "platform", "cisco_iosxe",
		"Scrapligo platform for --host devices (cisco_iosxe, cisco_nxos, arista_eos, juniper_junos, nokia_srlinux, nokia_sros, …)")

	// Output
	f.StringVarP(&bflags.dir, "dir", "d", "configs", "Output directory. Absolute paths used as-is; relative paths are placed under configs/ in the working directory")
	f.StringVarP(&bflags.tag, "tag", "t", "", "Optional tag included in saved filenames: <hostname>-<tag>-<timestamp>.cfg")
	f.BoolVar(&bflags.noTimestamp, "no-timestamp", false, "Omit timestamp from saved filenames (overwrites previous backup)")
	f.IntVarP(&bflags.workers, "workers", "w", 20, "Maximum number of concurrent SSH sessions")

	// Logging
	f.BoolVarP(&bflags.quiet, "quiet", "q", false, "Suppress console output (use with --syslog for silent cron/script use)")
	f.BoolVar(&bflags.useSyslog, "syslog", false, "Log to syslog in addition to (or instead of, with --quiet) console output")

	// Credentials
	f.StringVarP(&bflags.username, "username", "u", "", "SSH username")
	f.StringVarP(&bflags.password, "password", "p", "", "SSH password")
	f.StringVar(&bflags.enablePassword, "enable-password", "", "Enable/privileged-exec password")

	backupCmd.SetHelpFunc(backupHelp)
}

// flagOrder defines display order and grouping for backup --help.
var flagOrder = [][]string{
	{"file", "host", "platform"},
	{"dir", "tag", "no-timestamp", "workers"},
	{"quiet", "syslog"},
	{"username", "password", "enable-password"},
}

func backupHelp(cmd *cobra.Command, _ []string) {
	fmt.Println(cmd.Short)
	fmt.Printf("\nUsage:\n  %s\n", cmd.UseLine())
	if cmd.HasExample() {
		fmt.Printf("\nExamples:%s\n", cmd.Example)
	}
	fmt.Println("\nFlags:")

	type flagLine struct{ left, desc string }
	var lines []flagLine
	maxLeft := 0

	for _, group := range flagOrder {
		for _, name := range group {
			fl := cmd.Flags().Lookup(name)
			if fl == nil {
				continue
			}
			typeName := fl.Value.Type()
			if typeName == "bool" {
				typeName = ""
			}
			var left string
			if fl.Shorthand != "" {
				if typeName != "" {
					left = fmt.Sprintf("  -%s, --%s %s", fl.Shorthand, fl.Name, typeName)
				} else {
					left = fmt.Sprintf("  -%s, --%s", fl.Shorthand, fl.Name)
				}
			} else {
				if typeName != "" {
					left = fmt.Sprintf("      --%s %s", fl.Name, typeName)
				} else {
					left = fmt.Sprintf("      --%s", fl.Name)
				}
			}
			desc := fl.Usage
			if fl.DefValue != "" && fl.DefValue != "[]" && fl.DefValue != "false" {
				desc += fmt.Sprintf(" (default %q)", fl.DefValue)
			}
			lines = append(lines, flagLine{left, desc})
			if len(left) > maxLeft {
				maxLeft = len(left)
			}
		}
		lines = append(lines, flagLine{})
	}

	helpLeft := "      --help"
	lines = append(lines, flagLine{helpLeft, "help for " + cmd.Name()})
	if len(helpLeft) > maxLeft {
		maxLeft = len(helpLeft)
	}

	format := fmt.Sprintf("%%-%ds   %%s\n", maxLeft)
	for _, l := range lines {
		if l.left == "" {
			fmt.Println()
		} else {
			fmt.Printf(format, l.left, l.desc)
		}
	}
}

// ── Logging setup ─────────────────────────────────────────────────────────────

func setupLogging(quiet, useSyslog bool) {
	var handlers []slog.Handler

	if !quiet {
		handlers = append(handlers, slog.NewTextHandler(os.Stderr, nil))
	}

	if useSyslog {
		w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "netbot")
		if err != nil {
			// Can't write to syslog — fall back to stderr even in quiet mode.
			fmt.Fprintf(os.Stderr, "warning: could not open syslog: %v\n", err)
		} else {
			handlers = append(handlers, &syslogHandler{w: w})
		}
	}

	switch len(handlers) {
	case 0:
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	case 1:
		slog.SetDefault(slog.New(handlers[0]))
	default:
		slog.SetDefault(slog.New(&multiHandler{handlers: handlers}))
	}
}

// syslogHandler implements slog.Handler backed by log/syslog.
type syslogHandler struct {
	w *syslog.Writer
}

func (h *syslogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *syslogHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteByte(' ')
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		sb.WriteString(a.Value.String())
		return true
	})
	msg := sb.String()
	switch r.Level {
	case slog.LevelError:
		return h.w.Err(msg)
	case slog.LevelWarn:
		return h.w.Warning(msg)
	case slog.LevelDebug:
		return h.w.Debug(msg)
	default:
		return h.w.Info(msg)
	}
}

func (h *syslogHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *syslogHandler) WithGroup(name string) slog.Handler       { return h }

// multiHandler fans out to multiple slog.Handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			h.Handle(ctx, r) //nolint:errcheck
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{hs}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &multiHandler{hs}
}

// ── Entry point ───────────────────────────────────────────────────────────────

func runBackup(_ *cobra.Command, _ []string) error {
	setupLogging(bflags.quiet, bflags.useSyslog)

	var devices []device

	if bflags.file != "" {
		fromFile, err := devicesFromFile(bflags.file)
		if err != nil {
			return fmt.Errorf("reading job file: %w", err)
		}
		devices = append(devices, fromFile...)
	}

	for _, h := range bflags.hosts {
		devices = append(devices, device{
			Host:           h,
			Name:           h,
			Platform:       bflags.platform,
			Username:       bflags.username,
			Password:       bflags.password,
			EnablePassword: bflags.enablePassword,
		})
	}

	if len(devices) == 0 {
		return fmt.Errorf("no devices specified — use --host and/or --file")
	}

	// Resolve the output directory. Absolute paths are used as-is.
	// Relative paths are anchored under a "configs" folder in the working
	// directory so all saved configs share a common local root.
	outDir := resolveOutputDir(bflags.dir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory %q: %w", outDir, err)
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed []string
	)

	sem := make(chan struct{}, bflags.workers)

	for _, d := range devices {
		wg.Add(1)
		sem <- struct{}{}
		go func(d device) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := backup(d, outDir, bflags.tag, bflags.noTimestamp); err != nil {
				slog.Error("backup failed", "device", d.Name, "error", err)
				mu.Lock()
				failed = append(failed, d.Name)
				mu.Unlock()
			}
		}(d)
	}
	wg.Wait()

	slog.Info("done", "succeeded", len(devices)-len(failed), "failed", len(failed))

	if len(failed) > 0 {
		return fmt.Errorf("%d device(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

// ── Job file parsing ──────────────────────────────────────────────────────────

func devicesFromFile(path string) ([]device, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var jf jobFile
	if err := yaml.Unmarshal(data, &jf); err != nil {
		return nil, err
	}
	if len(jf.Devices) == 0 {
		return nil, fmt.Errorf("no devices found in %s", path)
	}

	out := make([]device, 0, len(jf.Devices))
	for _, d := range jf.Devices {
		if d.Host == "" {
			continue
		}
		out = append(out, device{
			Host:           d.Host,
			Name:           first(d.Name, d.Host),
			Platform:       first(d.Platform, "cisco_iosxe"),
			Command:        d.Command,
			Username:       first(d.Username, jf.Credentials.Username),
			Password:       first(d.Password, jf.Credentials.Password),
			EnablePassword: first(d.EnablePassword, jf.Credentials.EnablePassword),
		})
	}
	return out, nil
}

// ── Core backup logic ─────────────────────────────────────────────────────────

func commandForPlatform(plat string) string {
	switch plat {
	case "juniper_junos":
		return "show configuration"
	case "nokia_srlinux", "nokia_sros":
		return "info flat"
	default:
		return "show running-config"
	}
}

func backup(d device, outDir, tag string, noTimestamp bool) error {
	p, err := platform.NewPlatform(
		d.Platform,
		d.Host,
		options.WithAuthUsername(d.Username),
		options.WithAuthPassword(d.Password),
		options.WithAuthSecondary(d.EnablePassword),
		options.WithAuthNoStrictKey(),
	)
	if err != nil {
		return fmt.Errorf("initialising platform: %w", err)
	}

	drv, err := p.GetNetworkDriver()
	if err != nil {
		return fmt.Errorf("getting driver: %w", err)
	}

	if err := drv.Open(); err != nil {
		return fmt.Errorf("opening connection: %w", err)
	}
	defer drv.Close()

	cmd := d.Command
	if cmd == "" {
		cmd = commandForPlatform(d.Platform)
	}

	resp, err := drv.SendCommand(cmd)
	if err != nil {
		return fmt.Errorf("sending %q: %w", cmd, err)
	}
	if resp.Failed != nil {
		return fmt.Errorf("device reported failure for %q: %w", cmd, resp.Failed)
	}

	label := hostnameFromConfig(d.Platform, resp.Result)
	if label == "" {
		if prompt, err := drv.GetPrompt(); err == nil {
			label = hostnameFromPrompt(prompt)
		}
	}
	if label == "" {
		label = first(d.Host, d.Name)
	}

	filename := buildFilename(outDir, label, tag, noTimestamp)

	if err := os.WriteFile(filename, []byte(resp.Result), 0o644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	slog.Info("saved", "device", d.Name, "file", filename)
	return nil
}

// resolveOutputDir returns the final output directory path.
// Absolute paths are returned unchanged. Relative paths are joined
// under "configs/" so all relative output stays under a common local root.
// The default value of --dir is already "configs", so the default behaviour
// (no flag provided) produces "configs/" as before.
func resolveOutputDir(dir string) string {
	if filepath.IsAbs(dir) {
		return dir
	}
	// "configs" is the default — return it directly to avoid "configs/configs".
	if dir == "configs" {
		return "configs"
	}
	return filepath.Join("configs", dir)
}

// buildFilename constructs the output path from its components.
func buildFilename(outDir, label, tag string, noTimestamp bool) string {
	parts := []string{safeName(label)}
	if tag != "" {
		parts = append(parts, tag)
	}
	if !noTimestamp {
		parts = append(parts, time.Now().Format("20060102-150405"))
	}
	return filepath.Join(outDir, strings.Join(parts, "-")+".cfg")
}

// ── Hostname detection ────────────────────────────────────────────────────────

func hostnameFromConfig(plat, config string) string {
	var patterns []*regexp.Regexp
	switch plat {
	case "juniper_junos":
		patterns = []*regexp.Regexp{
			regexp.MustCompile(`(?m)^set system host-name\s+(\S+)`),
			regexp.MustCompile(`host-name\s+(\S+?);`),
		}
	case "nokia_srlinux", "nokia_sros":
		patterns = []*regexp.Regexp{
			regexp.MustCompile(`(?m)name[:\s]\s*"([^"]+)"`),
			regexp.MustCompile(`(?m)^\s*name\s+(\S+)`),
		}
	default:
		patterns = []*regexp.Regexp{
			regexp.MustCompile(`(?m)^hostname\s+(\S+)`),
		}
	}
	for _, p := range patterns {
		if m := p.FindStringSubmatch(config); m != nil {
			return m[1]
		}
	}
	return ""
}

func hostnameFromPrompt(prompt string) string {
	last := ""
	for _, line := range strings.Split(prompt, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			last = l
		}
	}
	if last == "" {
		return ""
	}
	last = strings.TrimRight(last, "#>$ \t\r_")
	if i := strings.Index(last, "@"); i >= 0 {
		last = last[i+1:]
	}
	if len(last) > 2 && (last[1] == ':' || last[1] == '_') {
		last = last[2:]
	}
	return last
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func safeName(s string) string {
	return strings.NewReplacer(
		" ", "_", "/", "_", "\\", "_", ":", "_",
	).Replace(s)
}
