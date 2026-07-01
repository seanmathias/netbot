# Ping File Reference

A ping file is a YAML file that specifies target hosts and optional session
settings for `netbot ping --file`. It is useful for saving a regularly-used
set of targets without typing them each time, and for sharing a monitoring
configuration between team members.

```bash
netbot ping --file targets.yaml
netbot ping --file targets.yaml --range 300s   # CLI flag overrides file setting
netbot ping --file targets.yaml 10.99.0.1      # ad-hoc extra target alongside file
```

---

## Structure

```yaml
interval: 1s
range: 90s
privileged: true

targets:
  - 8.8.8.8
  - 192.168.1.1
```

All fields except `targets` are optional. An empty or omitted `targets` list
is valid as long as at least one target is provided as a positional argument
on the command line.

---

## Fields

### `targets`

A list of hostnames or IP addresses to ping.

```yaml
targets:
  - 8.8.8.8
  - 8.8.4.4
  - gateway.example.com
  - 192.168.1.1
```

If the same host appears in both the file and as a positional argument,
it is deduplicated — it will only appear once in the table.

### `interval`

Time between consecutive pings to each target. Accepts Go duration syntax
(`500ms`, `1s`, `2s`). Minimum is `250ms`. Defaults to `1s`.

```yaml
interval: 500ms
```

### `range`

Duration of history shown in the timeline column. Accepts Go duration syntax
(`60s`, `5m`, `300s`). Defaults to `90s`.

```yaml
range: 300s
```

When the configured range doesn't fit at one cell per ping within the available
terminal width, pings are automatically bucketed so the entire range is always
visible — see [netbot.md](netbot.md) for details on how the timeline works.

### `privileged`

Whether to use raw ICMP sockets (`true`) or unprivileged UDP-based ICMP
(`false`). Defaults to `true`.

```yaml
privileged: false
```

Raw ICMP requires either root or the `cap_net_raw` capability on the binary:

```bash
sudo setcap cap_net_raw+ep ./netbot
```

Set `privileged: false` if you cannot grant capabilities but your system
supports unprivileged ICMP via UDP (most modern Linux systems do).

---

## Flag Precedence

CLI flags explicitly provided at runtime always override the corresponding
setting in the file. File settings apply only when the CLI flag was not
explicitly set.

```
CLI flag (if explicitly set)  >  file value  >  CLI flag default
```

For example, to override the range from the file without editing it:

```bash
netbot ping --file targets.yaml --range 600s
```

---

## Complete Example

```yaml
# netbot ping session — production edge routers
interval: 500ms
range: 5m
privileged: true

targets:
  - 203.0.113.1    # edge-router-1
  - 203.0.113.2    # edge-router-2
  - 198.51.100.1   # peer-as65000
  - 8.8.8.8        # google dns (baseline reference)
  - 1.1.1.1        # cloudflare dns (baseline reference)
```