#!/usr/bin/env bash
set -euo pipefail

# krapow: macOS host-isolated runner provisioning. Runs as the current user
# with cwd = ~/.krapow/runners/<name>/ and TMPDIR private to this runner.
# HOME stays the user's real home so codesign / xcrun notarytool / keychain-
# aware tools work; per-runner state still lives under cwd because the
# actions-runner agent stores config relative to cwd. No VM, no sudo.
#
# RUNNER_TOKEN is the GitHub registration token; injected here at the top so
# it never appears in argv.
export RUNNER_TOKEN='{{.RegToken}}'

# Host-isolation deliberately uses the host's existing toolchain — we don't
# install anything system-wide here. brew/gh/xcode must already be set up;
# `krapow doctor` checks for this before init.
command -v gh >/dev/null || {
    echo "krapow: 'gh' not on PATH — host-isolated runners use the host's gh; brew install gh" >&2
    exit 1
}

# Pin runner version by fetching the latest tag from GitHub's API. Baking a
# version in goes stale fast — GitHub enforces a rolling floor.
VER=$(curl -fsSL https://api.github.com/repos/actions/runner/releases/latest \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"].lstrip("v"))')

# Install relative to cwd (krapow set this to ~/.krapow/runners/<name>/).
# Don't use $HOME — that's the user's real home now, and an unqualified
# $HOME/actions-runner would dump the agent into ~/actions-runner.
mkdir -p ./actions-runner && cd ./actions-runner
curl -fsSL -o runner.tar.gz \
    "https://github.com/actions/runner/releases/download/v${VER}/actions-runner-osx-arm64-${VER}.tar.gz"
tar xzf runner.tar.gz
rm runner.tar.gz

# Register. macOS host already has the required tooling (gh, xcode, brew) —
# no installdependencies.sh equivalent needed.
./config.sh --unattended --replace \
    --url '{{.RepoURL}}' \
    --token "$RUNNER_TOKEN" \
    --name '{{.Name}}' \
    --labels '{{.Labels}}'

# NOTE: krapow installs its own LaunchAgent (com.monsterdept.krapow.<name>)
# pointing at the krapow-runner wrapper so macOS's background-activity
# notification reads as "krapow-runner" rather than "run.sh". svc.sh would
# bypass that wrapper.
