#!/usr/bin/env bash
set -euo pipefail

# krapow: macOS ARM runner provisioning. Runs inside the guest as `admin`
# (cirruslabs image default) via `ssh admin@<ip> bash -s`.
#
# Secrets are injected at the top so they never appear in argv or the SSH
# SendEnv whitelist (cirruslabs sshd doesn't AcceptEnv these names).
export RUNNER_TOKEN='{{.RegToken}}'

# GitHub CLI — cirruslabs macos-sequoia-xcode ships Homebrew but not `gh`.
# Release workflows shell out to `gh release create` etc. `brew install` is
# idempotent (no-op if already present) so safe to run every provision.
brew install gh

# Pin runner version by fetching the latest tag from GitHub's API. Baking a
# version into the image goes stale fast — GitHub enforces a rolling floor.
VER=$(curl -fsSL https://api.github.com/repos/actions/runner/releases/latest \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"].lstrip("v"))')

mkdir -p ~/actions-runner && cd ~/actions-runner
curl -fsSL -o runner.tar.gz \
    "https://github.com/actions/runner/releases/download/v${VER}/actions-runner-osx-arm64-${VER}.tar.gz"
tar xzf runner.tar.gz
rm runner.tar.gz

# Register. macOS images ship with required tooling baked in — no
# installdependencies.sh equivalent step needed.
./config.sh --unattended --replace \
    --url '{{.RepoURL}}' \
    --token "$RUNNER_TOKEN" \
    --name '{{.Name}}' \
    --labels '{{.Labels}}'

# Install as a launchd service so the runner survives this SSH session
# exiting. svc.sh on macOS writes a LaunchAgent and `launchctl load`s it.
./svc.sh install
./svc.sh start
