# netbot

A command-line tool for network automation and utility tasks. netbot connects to network devices over SSH and ICMP to perform day-to-day network engineering operations from a single binary.

[![Go Reference](https://pkg.go.dev/badge/github.com/seanmathias/netbot.svg)](https://pkg.go.dev/github.com/seanmathias/netbot)
![GitHub release](https://img.shields.io/github/v/release/seanmathias/netbot)
![Go version](https://img.shields.io/github/go-mod/go-version/seanmathias/netbot)
![License](https://img.shields.io/github/license/seanmathias/netbot)

## Features

- **`backup`** — saves the running configuration from one or more network devices in parallel, with job-file inventory support, configurable output naming, and syslog-friendly quiet mode for cron jobs.
- **`ping`** — continuous ICMP ping to one or more targets with a live, auto-refreshing terminal table. Each cell encodes latency (height), severity (color), and packet loss density (fade) at once, so trends and reliability issues are visible together even when compressing a long time range into limited terminal width.

Supported device platforms: Cisco IOS-XE, IOS-XR, NX-OS; Arista EOS; Juniper JunOS; Nokia SR Linux and SR OS.

## Installation

```bash
git clone https://github.com/seanmathias/netbot.git
cd netbot
make
```

This runs `go vet` and the test suite before building. To build directly:

```bash
go build -o netbot .
```

## Quick Start

```bash
# Back up a single device
./netbot backup --host 192.168.1.1 -u admin -p secret

# Back up a fleet from a job file
./netbot backup --file inventory.yaml --tag nightly --quiet --syslog

# Live ping dashboard
./netbot ping 8.8.8.8 1.1.1.1 192.168.1.1
```

See [netbot.md](netbot.md) for full command and flag documentation, and [job-file.md](job-file.md) for the `backup` job file format.

## Requirements

- Go 1.22+
- `ping` requires either root privileges or the `cap_net_raw` capability:
  ```bash
  sudo setcap cap_net_raw+ep ./netbot
  ```
  Alternatively, pass `--privileged=false` to use unprivileged UDP-based ICMP.

## Testing

```bash
go test ./...
```

## License

MIT