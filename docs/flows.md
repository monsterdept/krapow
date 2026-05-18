# krapow — state flows

Phase sequences for each (host, guest) combination, plus the one-time bake pipeline.

## Host × guest matrix

|                  | Linux host (amd64)                              | macOS host (arm64)                                              |
| ---------------- | ----------------------------------------------- | --------------------------------------------------------------- |
| **Linux guest**  | Incus + Ubuntu cloud (cloud-init drives setup)  | Tart + Ubuntu ARM (`cirruslabs/ubuntu-runner-arm64`)            |
| **Windows guest**| Incus + baked image (requires `bake` first)     | —                                                               |
| **macOS guest**  | —                                               | Tart + Sequoia (`cirruslabs/macos-sequoia-base`)                |

## Flows

### `krapow bake`

> Linux host only · produces `local:win-runner-base` · ~45–90 min · auto-runs from `init win` if image missing

```
Download  →  Prepare  →  Install  →  Provision  →  Publish
```

1. **Download** — fetch Windows Server 2022 Eval + virtio-win ISOs to `~/.krapow/cache`
2. **Prepare** — distrobuilder injects virtio drivers into `install.wim`; xorriso builds a tiny answer ISO with `autounattend.xml` + `setup-ssh.ps1`
3. **Install** — boot VM, run Windows Setup, wait for sshd (~30 min)
4. **Provision** — SSH in, install VS 2022 Build Tools + chocolatey (~15 min), then graceful PowerShell shutdown so Machine PATH flushes to disk
5. **Publish** — `incus publish` as alias

### `krapow init linux` (Incus path)

> Linux host · Ubuntu cloud image · cloud-init drives runner setup

```
Register  →  Boot  →  Cloud-init  →  Verify
```

1. **Register** — GitHub registration-token API call
2. **Boot** — `incus launch` with user-data injected; poll until Running with IPv4
3. **Cloud-init** — wait for cloud-init done (user-data installs & starts actions-runner)
4. **Verify** — poll GitHub `/actions/runners` until `status=online`

### `krapow init linux` (Tart path)

> macOS host · Ubuntu ARM via Tart

```
Register  →  Pull  →  Boot  →  Activate  →  Verify
```

1. **Register** — GitHub registration-token
2. **Pull** — `tart pull` image (long on cold cache)
3. **Boot** — `tart run` (detached), then wait for sshd
4. **Activate** — SSH-run actions-runner `config.sh` + `run.sh`
5. **Verify** — poll GitHub for `status=online`

### `krapow init mac`

> macOS host · macOS Sequoia via Tart · identical phase shape to Linux-on-Mac

```
Register  →  Pull  →  Boot  →  Activate  →  Verify
```

1. **Register** — GitHub registration-token
2. **Pull** — `tart pull` Sequoia image
3. **Boot** — `tart run`, then wait for sshd
4. **Activate** — SSH-run `config.sh` + `run.sh`
5. **Verify** — poll GitHub for `status=online`

### `krapow init win`

> Linux host · clones `local:win-runner-base` · SSH-push provisioning

```
Register  →  Boot  →  Partition  →  Activate  →  Verify
```

1. **Register** — GitHub registration-token
2. **Boot** — `incus launch local:win-runner-base`; wait for sshd (key already baked in)
3. **Partition** — `Resize-Partition C:` to fill disk
4. **Activate** — SSH-run actions-runner-win-x64 `config.cmd`
5. **Verify** — poll GitHub for `status=online`

> **Note:** if `local:win-runner-base` is missing, `bake` runs first (prompt unless `-y`).

## Structural patterns

### Common envelope across all init flows

```
Register  ──▶  [ provisioning middle ]  ──▶  Verify
```

### Two provisioning families

- **cloud-init (pull):** Linux-on-Linux only. `incus launch` injects user-data; the VM self-installs the runner; we passively wait in `Cloud-init` after `Boot`.
- **SSH push:** Linux-on-Mac, Mac, Windows. `Boot` includes the sshd wait; from there we SSH in and drive provisioning via the `Activate` phase.

### Image-prep variance

- **Tart paths:** explicit `Pull` phase (registry image, cold-cache slow).
- **Linux/Incus:** `incus launch` handles the registry pull invisibly — no separate phase.
- **Windows:** no pull, but the 5-phase `bake` pipeline runs *once* upstream to produce the local image.

### Truly OS-specific phases

`Partition` (Windows) is the only init phase that doesn't generalize. Exists because cloning the baked image gives you a 60 GiB C: regardless of destination disk size — PowerShell `Resize-Partition` is the fix. Everything else maps to a concept that appears in at least one other flow.
