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
| `--host` | `-h` | — | Device hostname or IP address. Repeatable for multiple devices. |
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

## Supported Platforms

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