#!/usr/bin/env bash
set -euo pipefail

# krapow: Linux ARM runner provisioning for tart-on-mac. Runs inside the guest
# as `admin` (cirruslabs image default) via `ssh admin@<ip> bash -s`. Differs
# from the cloud-init flow (Incus on Linux host) because tart doesn't run
# cloud-init out of the box — we drive the install over SSH instead.
export RUNNER_TOKEN='{{.RegToken}}'

# Latest runner version — same rationale as the macOS script.
VER=$(curl -fsSL https://api.github.com/repos/actions/runner/releases/latest \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"].lstrip("v"))' \
        2>/dev/null) \
    || VER=$(curl -fsSL https://api.github.com/repos/actions/runner/releases/latest \
        | sed -nE 's/.*"tag_name": "v?([^"]+)".*/\1/p' | head -1)

# Need a non-root user for the runner (config.sh refuses root). Cirrus's
# ubuntu-runner-arm64 image already has `admin`; create one if a different
# base sneaks in.
id runner >/dev/null 2>&1 || sudo useradd -m -s /bin/bash runner
echo 'runner ALL=(ALL) NOPASSWD: ALL' | sudo tee /etc/sudoers.d/runner >/dev/null
sudo chmod 0440 /etc/sudoers.d/runner

sudo mkdir -p /opt/actions-runner
sudo chown runner:runner /opt/actions-runner
cd /opt/actions-runner

sudo -u runner curl -fsSL -o runner.tar.gz \
    "https://github.com/actions/runner/releases/download/v${VER}/actions-runner-linux-arm64-${VER}.tar.gz"
sudo -u runner tar xzf runner.tar.gz
sudo -u runner rm runner.tar.gz

# installdependencies.sh handles libicu/libssl version variance across Ubuntu
# releases. Cheaper than pinning packages in this script.
sudo ./bin/installdependencies.sh

# LLVM toolchain — parity with the Incus cloud-init Linux path.
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y llvm clang lld

sudo -u runner ./config.sh --unattended --replace \
    --url '{{.RepoURL}}' \
    --token "$RUNNER_TOKEN" \
    --name '{{.Name}}' \
    --labels '{{.Labels}}'

# Register as a systemd service. svc.sh writes the unit file under
# /etc/systemd/system and `systemctl enable --now`s it.
sudo ./svc.sh install runner
sudo ./svc.sh start
