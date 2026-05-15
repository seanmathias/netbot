package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── hostnameFromConfig ────────────────────────────────────────────────────────

func TestHostnameFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		config   string
		want     string
	}{
		// Cisco IOS / IOS-XE / IOS-XR / NX-OS
		{
			name:     "cisco iosxe standard",
			platform: "cisco_iosxe",
			config:   "!\nhostname core-router-1\n!\nip domain name example.com\n",
			want:     "core-router-1",
		},
		{
			name:     "cisco iosxr standard",
			platform: "cisco_iosxr",
			config:   "hostname xr-pe-1\ninterface Loopback0\n",
			want:     "xr-pe-1",
		},
		{
			name:     "cisco nxos standard",
			platform: "cisco_nxos",
			config:   "!Command: show running-config\nhostname nexus-spine-1\n",
			want:     "nexus-spine-1",
		},
		{
			name:     "hostname mid-config not affected by leading content",
			platform: "cisco_iosxe",
			config:   "!\nversion 16.9\n!\nhostname dist-switch\n!\n",
			want:     "dist-switch",
		},
		{
			name:     "hostname with hyphen and numbers",
			platform: "cisco_iosxe",
			config:   "hostname r1-edge-01\n",
			want:     "r1-edge-01",
		},

		// Arista EOS
		{
			name:     "arista eos standard",
			platform: "arista_eos",
			config:   "! Command: show running-config\nhostname arista-spine-1\n!\n",
			want:     "arista-spine-1",
		},

		// Juniper JunOS — hierarchical format
		{
			name:     "juniper junos hierarchical",
			platform: "juniper_junos",
			config:   "system {\n    host-name juniper-vmx;\n    domain-name example.com;\n}\n",
			want:     "juniper-vmx",
		},
		{
			name:     "juniper junos hierarchical with extra whitespace",
			platform: "juniper_junos",
			config:   "system {\n    host-name  vqfx-re;\n}\n",
			want:     "vqfx-re",
		},

		// Juniper JunOS — display set format
		{
			name:     "juniper display set format",
			platform: "juniper_junos",
			config:   "set version 21.4R1.12\nset system host-name juniper-ex\nset system domain-name example.com\n",
			want:     "juniper-ex",
		},
		{
			name:     "juniper display set takes priority over hierarchical",
			platform: "juniper_junos",
			config:   "set system host-name vqfx-re\nhost-name other-name;\n",
			want:     "vqfx-re",
		},

		// Nokia SR Linux
		{
			name:     "nokia srlinux quoted name",
			platform: "nokia_srlinux",
			config:   "system {\n    name: \"nokia-srl-1\"\n}\n",
			want:     "nokia-srl-1",
		},
		{
			name:     "nokia srlinux unquoted name",
			platform: "nokia_srlinux",
			config:   "    name nokia-srl-leaf\n",
			want:     "nokia-srl-leaf",
		},

		// Nokia SR OS
		{
			name:     "nokia sros quoted name",
			platform: "nokia_sros",
			config:   "system\n    name \"nokia-sros-1\"\n",
			want:     "nokia-sros-1",
		},

		// No match / fallback cases
		{
			name:     "no hostname in output returns empty",
			platform: "cisco_iosxe",
			config:   "interface GigabitEthernet0/0\n ip address 10.0.0.1 255.255.255.0\n",
			want:     "",
		},
		{
			name:     "empty config returns empty",
			platform: "cisco_iosxe",
			config:   "",
			want:     "",
		},
		{
			name:     "hostname not matched mid-word",
			platform: "cisco_iosxe",
			config:   "! no-hostname-here\nip hostname-lookup\n",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostnameFromConfig(tt.platform, tt.config)
			if got != tt.want {
				t.Errorf("hostnameFromConfig(%q, ...) = %q, want %q", tt.platform, got, tt.want)
			}
		})
	}
}

// ── hostnameFromPrompt ────────────────────────────────────────────────────────

func TestHostnameFromPrompt(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{"cisco privileged", "router#", "router"},
		{"cisco privileged with space", "core-router-1# ", "core-router-1"},
		{"cisco user exec", "switch>", "switch"},
		{"arista privileged", "arista-spine-1#", "arista-spine-1"},
		{"juniper with user prefix privileged", "admin@juniper-ex#", "juniper-ex"},
		{"juniper with user prefix user exec", "admin@juniper-vmx> ", "juniper-vmx"},
		{"juniper netops user", "netops@vSRX>", "vSRX"},
		{"juniper vqfx multi-line", "{master:0}\nadmin@vqfx-re> ", "vqfx-re"},
		{"juniper vqfx master with backup", "{master:0}[edit]\nadmin@vqfx-re# ", "vqfx-re"},
		{"nokia srl context prefix colon", "A:nokia-srl#", "nokia-srl"},
		{"nokia srl context prefix underscore", "A_nokia-srl#", "nokia-srl"},
		{"empty prompt", "", ""},
		{"whitespace only", "   \t\n", ""},
		{"prompt with trailing newline", "router#\n", "router"},
		{"multi-line blank lines", "\n\n\nrouter#\n", "router"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostnameFromPrompt(tt.prompt)
			if got != tt.want {
				t.Errorf("hostnameFromPrompt(%q) = %q, want %q", tt.prompt, got, tt.want)
			}
		})
	}
}

// ── safeName ─────────────────────────────────────────────────────────────────

func TestSafeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple-name", "simple-name"},
		{"name with spaces", "name_with_spaces"},
		{"192.168.1.1", "192.168.1.1"},
		{"host/sub", "host_sub"},
		{`host\sub`, "host_sub"},
		{"host:name", "host_name"},
		{"normal-hostname-01", "normal-hostname-01"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := safeName(tt.input)
			if got != tt.want {
				t.Errorf("safeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ── first ─────────────────────────────────────────────────────────────────────

func TestFirst(t *testing.T) {
	tests := []struct {
		name string
		vals []string
		want string
	}{
		{"first non-empty wins", []string{"a", "b", "c"}, "a"},
		{"skips empty strings", []string{"", "b", "c"}, "b"},
		{"skips multiple empty", []string{"", "", "c"}, "c"},
		{"all empty returns empty", []string{"", "", ""}, ""},
		{"single value", []string{"only"}, "only"},
		{"no values", []string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := first(tt.vals...)
			if got != tt.want {
				t.Errorf("first(%v) = %q, want %q", tt.vals, got, tt.want)
			}
		})
	}
}

// ── commandForPlatform ────────────────────────────────────────────────────────

func TestCommandForPlatform(t *testing.T) {
	tests := []struct {
		platform string
		want     string
	}{
		{"cisco_iosxe", "show running-config"},
		{"cisco_iosxr", "show running-config"},
		{"cisco_nxos", "show running-config"},
		{"arista_eos", "show running-config"},
		{"juniper_junos", "show configuration"},
		{"nokia_srlinux", "info flat"},
		{"nokia_sros", "info flat"},
		{"unknown_platform", "show running-config"},
		{"", "show running-config"},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			got := commandForPlatform(tt.platform)
			if got != tt.want {
				t.Errorf("commandForPlatform(%q) = %q, want %q", tt.platform, got, tt.want)
			}
		})
	}
}

// ── buildFilename ─────────────────────────────────────────────────────────────

func TestBuildFilename(t *testing.T) {
	const dir = "/backups"
	const ts = "20260514-102942" // fixed time baked into expected values below

	// We can't freeze time.Now() without injecting it, so we test the shape
	// of the filename rather than its exact value when noTimestamp=false,
	// and test exact values when noTimestamp=true (no time component).
	tests := []struct {
		name        string
		label       string
		tag         string
		noTimestamp bool
		wantBase    string // exact basename when noTimestamp=true
		wantPrefix  string // prefix check when noTimestamp=false
		wantSuffix  string // always ".cfg"
	}{
		{
			name:        "label only, with timestamp",
			label:       "veos-sandbox",
			tag:         "",
			noTimestamp: false,
			wantPrefix:  "veos-sandbox-",
			wantSuffix:  ".cfg",
		},
		{
			name:        "label and tag, with timestamp",
			label:       "veos-sandbox",
			tag:         "weekly",
			noTimestamp: false,
			wantPrefix:  "veos-sandbox-weekly-",
			wantSuffix:  ".cfg",
		},
		{
			name:        "label only, no timestamp",
			label:       "core-router",
			tag:         "",
			noTimestamp: true,
			wantBase:    "core-router.cfg",
		},
		{
			name:        "label and tag, no timestamp",
			label:       "core-router",
			tag:         "prod",
			noTimestamp: true,
			wantBase:    "core-router-prod.cfg",
		},
		{
			name:        "label with unsafe chars, no timestamp",
			label:       "host:name",
			tag:         "",
			noTimestamp: true,
			wantBase:    "host_name.cfg",
		},
		{
			name:        "ip address label, no timestamp",
			label:       "192.168.1.1",
			tag:         "audit",
			noTimestamp: true,
			wantBase:    "192.168.1.1-audit.cfg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFilename(dir, tt.label, tt.tag, tt.noTimestamp)
			base := filepath.Base(got)

			if tt.noTimestamp {
				if base != tt.wantBase {
					t.Errorf("buildFilename base = %q, want %q", base, tt.wantBase)
				}
			} else {
				if !strings.HasPrefix(base, tt.wantPrefix) {
					t.Errorf("buildFilename base = %q, want prefix %q", base, tt.wantPrefix)
				}
				if !strings.HasSuffix(base, tt.wantSuffix) {
					t.Errorf("buildFilename base = %q, want suffix %q", base, tt.wantSuffix)
				}
			}
			_ = ts // suppress unused warning
		})
	}
}

// ── devicesFromFile ───────────────────────────────────────────────────────────

func writeJobFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestDevicesFromFile(t *testing.T) {
	t.Run("minimal device uses global credentials", func(t *testing.T) {
		path := writeJobFile(t, `
credentials:
  username: admin
  password: secret
  enable_password: en_secret
devices:
  - host: 192.168.1.1
    name: router-1
`)
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(devices) != 1 {
			t.Fatalf("want 1 device, got %d", len(devices))
		}
		d := devices[0]
		assertEqual(t, "Host", d.Host, "192.168.1.1")
		assertEqual(t, "Name", d.Name, "router-1")
		assertEqual(t, "Username", d.Username, "admin")
		assertEqual(t, "Password", d.Password, "secret")
		assertEqual(t, "EnablePassword", d.EnablePassword, "en_secret")
	})

	t.Run("per-device credentials override global", func(t *testing.T) {
		path := writeJobFile(t, `
credentials:
  username: global-user
  password: global-pass
  enable_password: global-en
devices:
  - host: 10.0.0.1
    username: device-user
    password: device-pass
    enable_password: device-en
`)
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := devices[0]
		assertEqual(t, "Username", d.Username, "device-user")
		assertEqual(t, "Password", d.Password, "device-pass")
		assertEqual(t, "EnablePassword", d.EnablePassword, "device-en")
	})

	t.Run("partial per-device credentials fall back to global", func(t *testing.T) {
		path := writeJobFile(t, `
credentials:
  username: global-user
  password: global-pass
devices:
  - host: 10.0.0.1
    username: device-user
`)
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, "Username", devices[0].Username, "device-user")
		assertEqual(t, "Password (fallback)", devices[0].Password, "global-pass")
	})

	t.Run("name defaults to host when omitted", func(t *testing.T) {
		path := writeJobFile(t, "devices:\n  - host: 10.0.0.1\n")
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, "Name", devices[0].Name, "10.0.0.1")
	})

	t.Run("platform defaults to cisco_iosxe when omitted", func(t *testing.T) {
		path := writeJobFile(t, "devices:\n  - host: 10.0.0.1\n")
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, "Platform", devices[0].Platform, "cisco_iosxe")
	})

	t.Run("explicit platform is preserved", func(t *testing.T) {
		path := writeJobFile(t, "devices:\n  - host: 10.0.0.1\n    platform: juniper_junos\n")
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, "Platform", devices[0].Platform, "juniper_junos")
	})

	t.Run("nokia srlinux platform is preserved", func(t *testing.T) {
		path := writeJobFile(t, "devices:\n  - host: 10.0.0.1\n    platform: nokia_srlinux\n")
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, "Platform", devices[0].Platform, "nokia_srlinux")
	})

	t.Run("custom command is preserved", func(t *testing.T) {
		path := writeJobFile(t, `
devices:
  - host: 10.0.0.1
    platform: juniper_junos
    command: "show configuration | display set"
`)
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, "Command", devices[0].Command, "show configuration | display set")
	})

	t.Run("device without host is skipped", func(t *testing.T) {
		path := writeJobFile(t, `
devices:
  - host: 10.0.0.1
    name: valid
  - name: no-host-device
  - host: 10.0.0.2
    name: also-valid
`)
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(devices) != 2 {
			t.Errorf("want 2 devices (host-less entry skipped), got %d", len(devices))
		}
	})

	t.Run("multiple devices all parsed", func(t *testing.T) {
		path := writeJobFile(t, `
credentials:
  username: admin
  password: secret
devices:
  - host: 10.0.0.1
  - host: 10.0.0.2
  - host: 10.0.0.3
`)
		devices, err := devicesFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(devices) != 3 {
			t.Errorf("want 3 devices, got %d", len(devices))
		}
	})

	t.Run("empty devices list returns error", func(t *testing.T) {
		path := writeJobFile(t, "credentials:\n  username: admin\ndevices: []\n")
		_, err := devicesFromFile(path)
		if err == nil {
			t.Error("expected error for empty devices list, got nil")
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		path := writeJobFile(t, "credentials: [this is not valid yaml\n  username: broken\n")
		_, err := devicesFromFile(path)
		if err == nil {
			t.Error("expected error for invalid YAML, got nil")
		}
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		_, err := devicesFromFile("/tmp/netbot-does-not-exist-xyz.yaml")
		if err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})
}

// ── CLI flags ─────────────────────────────────────────────────────────────────

func TestBackupFlagDefaults(t *testing.T) {
	f := backupCmd.Flags()

	checkString := func(flag, want string) {
		t.Helper()
		got, err := f.GetString(flag)
		if err != nil {
			t.Fatalf("getting --%s: %v", flag, err)
		}
		if got != want {
			t.Errorf("--%s default = %q, want %q", flag, got, want)
		}
	}
	checkInt := func(flag string, want int) {
		t.Helper()
		got, err := f.GetInt(flag)
		if err != nil {
			t.Fatalf("getting --%s: %v", flag, err)
		}
		if got != want {
			t.Errorf("--%s default = %d, want %d", flag, got, want)
		}
	}
	checkBool := func(flag string, want bool) {
		t.Helper()
		got, err := f.GetBool(flag)
		if err != nil {
			t.Fatalf("getting --%s: %v", flag, err)
		}
		if got != want {
			t.Errorf("--%s default = %v, want %v", flag, got, want)
		}
	}

	checkString("dir", "configs")
	checkString("platform", "cisco_iosxe")
	checkString("tag", "")
	checkInt("workers", 20)
	checkBool("no-timestamp", false)
	checkBool("quiet", false)
	checkBool("syslog", false)
}

func TestHostFlagShorthand(t *testing.T) {
	fl := backupCmd.Flags().Lookup("host")
	if fl == nil {
		t.Fatal("--host flag not found")
	}
	if fl.Shorthand != "H" {
		t.Errorf("--host shorthand = %q, want %q", fl.Shorthand, "H")
	}
}

func TestBackupCmdRequiresDevices(t *testing.T) {
	root.SetArgs([]string{"backup"})
	err := root.Execute()
	if err == nil {
		t.Error("expected error when no devices specified, got nil")
	}
	if !strings.Contains(err.Error(), "no devices specified") {
		t.Errorf("error = %q, want it to contain 'no devices specified'", err.Error())
	}
}

// ── resolveOutputDir ─────────────────────────────────────────────────────────

func TestResolveOutputDir(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Default — returned as-is to avoid configs/configs
		{"configs", "configs"},
		// Relative paths anchored under configs/
		{"weekly", filepath.Join("configs", "weekly")},
		{"2026/nightly", filepath.Join("configs", "2026", "nightly")},
		// Absolute paths pass through unchanged
		{"/backups/network", "/backups/network"},
		{"/tmp/netbot", "/tmp/netbot"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolveOutputDir(tt.input)
			if got != tt.want {
				t.Errorf("resolveOutputDir(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertEqual(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", field, got, want)
	}
}
