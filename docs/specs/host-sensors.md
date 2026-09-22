# Spec: Host sensors

- **Status:** approved
- **Owns:** `internal/sensor/load`, `internal/sensor/memory`, `internal/sensor/uptime`,
  `internal/sensor/systemd` (agent): what each reads, and the metric ids it reports
- **Decisions:** [0003](../decisions/0003-sensors-are-modules.md),
  [0010](../decisions/0010-agent-configuration.md),
  [0033](../decisions/0033-a-subject-is-a-series.md)

## Purpose

Four sensors that read the state of the machine itself, alongside the
[disk sensor](disk-sensor.md): how busy it is, how much memory it has left, how long since
it booted, and whether any service failed. Like every sensor, they decide nothing: the
series they produce have no level until someone sets one on the series' page
([thresholds.md](thresholds.md)).

None of them takes a parameter. What runs where, and how often, is the hub's configuration;
the compiled-in profiles and intervals that run them are [hub-config.md](hub-config.md)'s.

## Measurements

No metric carries a label: each is one series per node. The unit is the one the id declares
([history.md](history.md#wire-format)).

| Sensor | Metric | Value |
|---|---|---|
| `load` | `load.avg_1m`, `load.avg_5m`, `load.avg_15m` | the system's load averages, as `uptime` prints them, rounded to two decimals |
| `memory` | `memory.available_pct` | the share of memory the system itself reports as available, rounded to two decimals |
| `memory` | `memory.available_bytes` | that share of the total memory the system reports, in whole bytes |
| `uptime` | `uptime.boot_seconds` | whole seconds from the boot time the system reports to the agent's clock, truncated |
| `systemd` | `systemd.failed_units` | the number of units the system manager holds in the `failed` state — what `systemctl --failed` lists |

**Available memory is the system's own figure, never a formula of ours.** Free memory alone
reads near zero on any machine that has been up an hour, because both systems fill idle
memory with cache they give back on demand; what is available instead of free is a judgement,
and the operating system is the one that makes it:

| Platform | Share available | Total memory |
|---|---|---|
| Linux | `MemAvailable` ÷ `MemTotal`, from `/proc/meminfo` | `MemTotal` — what the kernel manages, a little under the installed memory |
| macOS | `kern.memorystatus_level`, the free percentage the kernel's memory-pressure decisions and `memory_pressure` use | `hw.memsize`, the installed memory |

On macOS the kernel answers a percentage only, so the byte figure is derived from it; on
Linux the percentage is derived from the bytes. Activity Monitor's "memory used" is not
used: it rests on app memory, which no public interface reports, and on a machine with a
full compressor it reads a fraction of what the kernel considers free.

**Failed units are a count, not a list.** A count is one series that a threshold of `above 0`
turns into an alert; one series per unit would leave every unit that ever failed as a row of
its own. Only the system manager is counted, never a user's `systemd --user` instance.

## Behaviour

One row = one test. Anchors: `spec: host-sensors.md#<heading>`.

### Load

| Machine state | Collected |
|---|---|
| the system reports load averages of 0.5, 1.25 and 2.125 | `load.avg_1m` 0.5, `load.avg_5m` 1.25, `load.avg_15m` 2.13 |
| the load averages cannot be read | no measurements and an error the agent logs |

### Memory

| Machine state | Collected |
|---|---|
| Linux reporting `MemTotal` 8 000 000 kB and `MemAvailable` 2 000 000 kB | `memory.available_bytes` 2 048 000 000, `memory.available_pct` 25 |
| Linux without `MemAvailable` (a kernel older than 3.14) | no measurements and an error the agent logs: an estimate of our own would disagree with the system |
| macOS reporting a free level of 50 on 16 GiB installed | `memory.available_pct` 50, `memory.available_bytes` 8 589 934 592 |
| the figures cannot be read | no measurements and an error the agent logs |

### Uptime

| Machine state | Collected |
|---|---|
| booted 3 days, 2 hours and 0.9 s before the agent's clock | `uptime.boot_seconds` 266 400 |
| booted again since the last collection, 10 minutes before the agent's clock | `uptime.boot_seconds` 600: the value counts from the new boot |
| the boot time is later than the agent's clock — a clock set back | no measurements and an error the agent logs: a negative uptime is not a reading |
| the boot time cannot be read | no measurements and an error the agent logs |

### Failed units

| Machine state | Collected |
|---|---|
| no unit failed | `systemd.failed_units` 0, so a threshold has something to leave its level on |
| three units failed | `systemd.failed_units` 3 |
| systemd does not answer before the collection is cancelled | no measurements and an error the agent logs |
| the machine was not booted with systemd, and the sensor is enabled for it anyway | no measurements and no error: a sensor the configuration runs where it cannot apply stays quiet |

### Applicability

| Machine | Manifest entry |
|---|---|
| any supported platform | `load`, `memory` and `uptime`, applicable |
| Linux booted with systemd — `/run/systemd/system` is a directory, as `sd_booted` tests | `systemd`, applicable |
| macOS, or Linux without systemd | `systemd`, not applicable |

## Invariants

- Collection is read-only and cheap: no process tree walked, nothing done per unit or per
  process.
- No sensor holds state between collections; each reports what the system says now.
- A sensor that cannot read its value returns an error and no measurements, never a zero:
  a zero is a reading, and `0` failed units or `0` bytes available would be a false one.
- Every measurement names the sensor that produced it, so its series ages by that sensor's
  interval ([0033](../decisions/0033-a-subject-is-a-series.md)).
- No sensor needs cgo: the darwin halves read `sysctl`, so the one cgo file stays the disk
  sensor's ([0014](../decisions/0014-macos-available-space.md)).

## Edge cases

- **An agent too old to contain a sensor** is delivered it anyway and runs nothing for it;
  the node's series simply do not appear until the agent updates itself
  ([0028](../decisions/0028-agents-follow-a-target-the-hub-serves.md)).
- **A reboot** shows as a drop in `uptime.boot_seconds` between two points; an `uptime.boot_seconds below 1800`
  threshold turns it into an alert that clears itself once the machine has been up long
  enough.
- **Load averages are not comparable between machines**: 4 is idle on sixteen cores and
  saturation on one. That is why the value is set per series on its page rather than
  shipped as a default.
- **A laptop that sleeps** does not count sleep as load: the averages freeze, so the first
  reading after wake carries the load from before sleep. Its uptime keeps counting through
  sleep on both systems: uptime measures since boot, not since wake.
- **In a container** the sensors report what the container's `/proc` shows — host-wide
  unless something like lxcfs virtualises it — and a systemd inside a system container
  counts the container's units. The monitor does not correct for either.
- **The two platforms' memory percentages are not the same judgement**: each is its own
  system's answer, and a threshold set on one machine's series says nothing about another's.

## Out of scope

- Backups → [#51](https://github.com/pravbeseda/monitor/issues/51).
- Per-process CPU or memory, swap, temperatures, network — later sensors, not these.
- Which unit failed, and restarting it: the monitor observes, it does not operate.
- Normalising load by core count: a derived series belongs to the semantic engine, not to a
  sensor.

## Open questions

None.
