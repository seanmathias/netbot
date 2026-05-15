# Job File Reference

A job file is a YAML file that defines the devices and credentials for a netbot operation. It allows you to manage a device inventory separately from the command line and commit it to version control.

Job files are passed to netbot using the `--file` / `-f` flag:

```bash
netbot backup --file inventory.yaml
netbot backup --file /etc/netbot/prod.yaml --tag weekly --dir /backups
```

---

## Structure

```yaml
credentials:   # Shared credentials applied to all devices by default
  ...

devices:       # List of target devices
  - ...
```

---

## `credentials`

Shared SSH credentials used for any device that does not specify its own.

```yaml
credentials:
  username: admin
  password: secret
  enable_password: en_secret
```

| Field | Type | Description |
|---|---|---|
| `username` | string | SSH login username. |
| `password` | string | SSH login password. |
| `enable_password` | string | Password sent in response to an enable/privileged-exec prompt. Omit if not required. |

### Credential Precedence

```
per-device field  >  credentials section  >  CLI flag
```

---

## `devices`

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host` | string | **Yes** | — | Hostname or IP address used for the SSH connection. |
| `name` | string | No | Same as `host` | Human-readable label used in log output. |
| `platform` | string | No | `cisco_iosxe` | Scrapligo platform name. See [Supported Platforms](netbot.md#supported-platforms). |
| `username` | string | No | `credentials.username` | Per-device username override. |
| `password` | string | No | `credentials.password` | Per-device password override. |
| `enable_password` | string | No | `credentials.enable_password` | Per-device enable password override. |
| `command` | string | No | Platform default | Command used to retrieve the configuration. |

### Default Commands by Platform

| Platform | Default Command |
|---|---|
| `juniper_junos` | `show configuration` |
| `nokia_srlinux` | `info flat` |
| `nokia_sros` | `info flat` |
| All others | `show running-config` |

---

## Complete Example

```yaml
credentials:
  username: admin
  password: secret
  enable_password: en_secret

devices:

  # Cisco IOS-XE — minimal entry, uses shared credentials
  - host: 192.168.1.1
    name: core-router-1

  # Cisco NX-OS
  - host: 192.168.1.10
    name: nexus-1
    platform: cisco_nxos

  # Arista EOS
  - host: 192.168.1.20
    name: arista-spine-1
    platform: arista_eos

  # Juniper JunOS — default command (show configuration)
  - host: 192.168.1.30
    name: juniper-vmx
    platform: juniper_junos

  # Juniper JunOS — display set output format
  - host: 192.168.1.31
    name: juniper-ex
    platform: juniper_junos
    command: show configuration | display set

  # Nokia SR Linux — default command (info flat)
  - host: 192.168.1.40
    name: nokia-srl-1
    platform: nokia_srlinux

  # Nokia SR OS — default command (info flat)
  - host: 192.168.1.41
    name: nokia-sros-1
    platform: nokia_sros

  # Device with its own credentials
  - host: 10.0.0.50
    name: mgmt-oob-switch
    username: netops
    password: different_password
    enable_password: different_enable
```

---

## Tips

**`--no-timestamp` keeps a single current copy.** When combined with `--tag`, you can maintain named snapshots without accumulating dated files:

```bash
netbot backup --file inventory.yaml --tag current --no-timestamp
# produces: core-router-1-current.cfg (overwritten each run)
```

**Mix job file devices with ad-hoc hosts.** You can combine `--file` with one or more `--host` flags. Ad-hoc hosts use the platform and credentials provided via CLI flags.

```bash
netbot backup --file inventory.yaml --host 10.99.0.1 -u admin -p secret
```

**Store credentials securely.** Avoid committing job files with plaintext passwords to version control. Restrict file permissions at minimum:

```bash
chmod 600 inventory.yaml
```