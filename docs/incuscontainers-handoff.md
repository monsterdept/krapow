# Phase 3 handoff — Linux containers via Incus

This doc is the resume-point for the Phase 3 work that has to happen on a
Linux host (where Incus actually exists). Phases 1 and 2 are landed and
verified on macOS; Phase 3 is fresh — no code has been written yet.

## Status

- **Phase 1** (`Isolation` field on `state.Runner` + `--isolation` flag) — landed.
- **Phase 2** (macOS host-isolation as default for `init mac`) — landed; verified end-to-end with a Tauri codesign CI run.
- **Phase 3** (Linux containers as default for `init linux` on Linux hosts) — not started.
- **Phase 4** (Apple Containerization for `init linux` on macOS hosts, macOS 26+) — deferred indefinitely.

## What Phase 3 ships

`krapow init linux` on a Linux host should default to `incus launch <image>`
(system container) instead of `incus launch <image> --vm`. Sub-second boot,
much smaller disk footprint, same `cmd/shell.go`/`stop.go`/`start.go` code
paths (Incus's `exec`/`stop`/`start`/`delete` work identically for both
instance types). `--isolation vm` is the escape hatch for workflows that
need nested KVM, `modprobe`, loop mounts, etc.

## Files to touch

### `internal/incus/incus.go`

Add `LaunchContainer(image, name string, configKV, deviceKV map[string]string) error`
modeled on `LaunchVM` at line 42. Identical body minus the `"--vm"` flag in
the `args` slice. Keep `LaunchVM` for the escape hatch and for Windows.

### `cmd/init.go::resolveIsolation`

Currently linuxKind allows only `{"vm"}`. After Phase 3 it should depend on
the host:

```go
linuxAllowed := []string{"vm"}
if runtime.GOOS == "linux" {
    linuxAllowed = []string{"container", "vm"}
}
allowed := map[kind][]string{
    linuxKind:   linuxAllowed,
    macKind:     {"host", "vm"},
    windowsKind: {"vm"},
}
```

On macOS, `init linux` keeps defaulting to `vm` (the Tart Linux-ARM path) —
Phase 4 is the one that would prepend `container` there, and Phase 4 is
deferred.

### `cmd/init.go::doInitLinux`

Branch on `ic.isolation`:

- **container**: `incus.LaunchContainer` with `user.user-data` + `limits.cpu` + `limits.memory`. Drop `security.secureboot` (VM-only) and `root.size` (containers grow on demand; the 75 GiB number was GitHub-hosted-runner parity).
- **vm**: the existing `LaunchVM` call, unchanged.

Same `state.Save` afterward; `Backend` stays empty (defaults to `"incus"`)
for both. `waitForLinuxCloudInit` already works for both — the
`images:ubuntu/noble/cloud` image runs cloud-init in either mode.

### `cmd/doctor.go::checkVsock`

Currently always-on on Linux. Optional polish: demote to skipped when no
`isolation=vm` linux runners exist (vsock is VM-only). Not blocking.

## Decisions already made

- **Container default on Linux hosts**: yes — instant boot wins for the typical CI workload; the kernel-isolation tradeoff is acceptable since the runner host IS the user's machine.
- **Container type**: Incus *system container* (full init, persistent rootfs), not Docker. Krapow already speaks Incus.
- **Disk sizing**: no `root.size`. Containers grow on demand up to the pool's capacity.
- **`security.secureboot=false`**: VM-only. Don't pass for containers.
- **State schema**: unchanged. `Isolation` was added in Phase 1; `Backend` stays `"incus"` for both isolation modes.

## Verification on the Linux host

```sh
just lint && just test
just install
krapow doctor                                            # baseline
krapow init linux --repo <owner>/<repo>                  # defaults to container
krapow status                                            # ISOLATION column shows "container"
# Run a real workflow to confirm cloud-init + actions-runner come up
krapow destroy <name>                                    # cleanup
krapow init linux --repo <owner>/<repo> --isolation vm   # confirm escape hatch
```

## Lesson from Phase 2

We shipped Phase 2 with `HOME=<per-runner-dir>` in the LaunchAgent plist on
the assumption that "sandboxing HOME is a hygiene boundary." It broke
`codesign` because macOS's keychain search list is read from
`~/Library/Preferences/com.apple.security.plist` — HOME-relative. The fix
was to stop overriding HOME and just set TMPDIR.

The Linux container analogue: don't try to import host-side state into the
container (host home, ssh keys, etc.) for the sake of convenience. Anything
the workflow needs should come in via GH secrets. The container's boundary
is namespaces + cgroups; trust it.

## Where to pick up

- `TaskCreate` history: tasks #12 (`Phase 3.1: LaunchContainer in internal/incus`) and #13 (`Phase 3.2: flip linux default to container on Linux hosts`) are still tracked.
- No Phase 3 code has been written. Fresh start.
- Branch state: clean on `main` (commit that includes this doc).
