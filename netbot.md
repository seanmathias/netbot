# netbot

**netbot** is a command-line tool for network automation and utility tasks. It connects to network devices over SSH to perform operations such as configuration backup, using [scrapligo](https://github.com/scrapli/scrapligo) for device interaction.

---

## Installation

```bash
git clone https://github.com/yourorg/netbot.git
cd netbot
make
```

Or build directly:

```bash
go build -o netbot .
```

Move the binary somewhere on your `PATH`:

```bash
mv netbot /usr/local/bin/
```

---

## Global Usage

```
netbot <command> [flags]
```

Run `netbot --help` or `netbot <command> --help` at any time for inline help.

---

## Commands

### `backup`

Connects to one or more network devices and saves their running configuration to disk.

```
netbot backup [flags]
```

Devices can be specified directly via flags or through a [job file](job-file.md). Both can be combined — job file devices and `--host` devices are merged into one run.

Backups run **in parallel**. The `--workers` flag controls the maximum number of concurrent SSH sessions (default: 20).

#### Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--file` | `-f` | — | Path to a YAML job file defining devices and credentials. |
| `--host` | `-H` | — | Device hostname or IP address. Repeatable for multiple devices. |
| `--platform` | — | `cisco_iosxe` | Scrapligo platform name for `--host` devices. See [Supported Platforms](#supported-platforms). |
| `--dir` | `-d` | `configs` | Directory to write configuration files to. Relative or absolute. Created if it does not exist. |
| `--tag` | `-t` | — | Optional tag inserted into saved filenames: `<hostname>-<tag>-<timestamp>.cfg`. |
| `--no-timestamp` | — | `false` | Omit the timestamp from filenames. Files are overwritten on each run. |
| `--workers` | `-w` | `20` | Maximum number of concurrent SSH sessions. |
| `--quiet` | `-q` | `false` | Suppress all console output. Pair with `--syslog` to retain logging. |
| `--syslog` | — | `false` | Log to syslog (facility: daemon) in addition to, or instead of, the console. |
| `--username` | `-u` | — | SSH username. |
| `--password` | `-p` | — | SSH password. |
| `--enable-password` | — | — | Password sent in response to an enable/privileged-exec prompt. |

#### Output File Naming

```
# Default
<hostname>-<timestamp>.cfg

# With --tag
<hostname>-<tag>-<timestamp>.cfg

# With --no-timestamp
<hostname>.cfg

# With --tag and --no-timestamp
<hostname>-<tag>.cfg
```

Timestamps use the format `YYYYMMDD-HHMMSS`. The hostname is derived from the device's own configuration output, then from the SSH prompt, then from the connection address as a last resort.

#### Running Quietly (cron / scripts)

```bash
# Silent run, logs to syslog
netbot backup --file /etc/netbot/inventory.yaml --quiet --syslog

# Example crontab — nightly at 2am
0 2 * * * /usr/local/bin/netbot backup --file /etc/netbot/inventory.yaml --quiet --syslog --tag nightly
```

Logs are written with the identifier `netbot`, facility `daemon`.

#### Examples

```bash
netbot backup --host 192.168.1.1 -u admin -p secret
netbot backup --host r1 --host r2 -u admin -p secret --enable-password cisco
netbot backup --file inventory.yaml --dir /backups/network
netbot backup --file inventory.yaml --tag current --no-timestamp
netbot backup --file inventory.yaml --workers 50 --dir /backups/network
netbot backup --file inventory.yaml --quiet --syslog --tag nightly
```

---

### `ping`

Sends continuous ICMP pings to one or more targets and displays a live, auto-refreshing table.

```
netbot ping <target> [target ...] [flags]
```

Press `q` or `Ctrl+C` to exit.

#### Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--interval` | `-i` | `1s` | Time between pings per target. Minimum `250ms`. |
| `--range` | `-r` | `90s` | Duration of history shown in the timeline column. |
| `--privileged` | — | `true` | Use raw ICMP sockets. Set `false` to fall back to unprivileged UDP-based ICMP. |

#### Privileges

ICMP normally requires elevated privileges. Either run as root, or grant the capability to the binary once so it can be run as a regular user:

```bash
sudo setcap cap_net_raw+ep ./netbot
```

Alternatively, pass `--privileged=false` to use UDP-based ICMP, which works without root on most Linux systems but may behave differently across platforms and firewalls.

#### Reading the Timeline

Each cell in the timeline represents one bucket of time. When the configured `--range` doesn't fit one cell per ping in the available terminal width, multiple consecutive pings are automatically binned together into a single cell so the *entire* requested range is always represented — a long `--range` is never silently truncated to whatever recent window happens to fit.

Every cell encodes three independent signals at once:

| Signal | What it shows |
|---|---|
| **Height** (`▁▂▃▄▅▆▇█`) | RTT magnitude — taller bars mean higher latency. |
| **Color** (green → yellow → orange → red) | RTT severity tier, same thresholds as the table below. |
| **Fade** (vivid → gray) | Packet loss density within that cell. Vivid means every ping in the cell got a reply; a washed-out, faded color means some were lost. |

A cell is left **blank** only when every ping in its bucket was lost — there's no RTT data to show at all in that case.

This means a cell can distinguish "high latency but fully reliable" (tall, vivid red) from "the same high latency, but also lossy" (tall, faded red) — something a single flat-colored marker can't convey, especially once several pings have been compressed into one cell.

#### RTT Severity Thresholds

| Latency | Color |
|---|---|
| < 20ms | Green |
| 20–60ms | Yellow |
| 60–120ms | Orange |
| ≥ 120ms | Red |

These thresholds are tuned for typical LAN/WAN round-trip times. The height scale tops out (full-height bar) at 240ms (2x the orange threshold).

#### Bucket Stability

When a cell represents multiple binned pings, its appearance is anchored to those specific pings permanently — already-rendered history never silently changes color or shape as the live window keeps scrolling forward. Only the single newest (rightmost, still-filling) cell updates as more pings land in it; every cell to its left is frozen the moment a new bucket begins.

#### Examples

```bash
# Single target, default 90s window
netbot ping 192.168.1.1

# Multiple targets at once
netbot ping 8.8.8.8 1.1.1.1 192.168.1.1

# Faster polling, shorter window
netbot ping 8.8.8.8 --interval 500ms --range 60s

# Five-minute compressed view
netbot ping 8.8.8.8 --range 300s

# No root / no setcap — unprivileged UDP mode
netbot ping 8.8.8.8 --privileged=false
```

---

| Platform | Value | Default Command |
|---|---|---|
| Cisco IOS-XE | `cisco_iosxe` | `show running-config` |
| Cisco IOS-XR | `cisco_iosxr` | `show running-config` |
| Cisco NX-OS | `cisco_nxos` | `show running-config` |
| Arista EOS | `arista_eos` | `show running-config` |
| Juniper JunOS | `juniper_junos` | `show configuration` |
| Nokia SR Linux | `nokia_srlinux` | `info flat` |
| Nokia SR OS | `nokia_sros` | `info flat` |

---

## Job Files

See [job-file.md](job-file.md) for the full job file reference.