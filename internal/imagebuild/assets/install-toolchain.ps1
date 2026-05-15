# Installs the krapow universal Windows toolchain into the base image during
# bake (post-OS-install, pre-sysprep). Aims for parity with what GitHub-hosted
# windows-latest ships:
#
#   - Visual Studio 2022 Build Tools with the C++ workload + ARM64 cross-tools
#   - Chocolatey package manager
#
# Anything beyond this — language toolchains (Rust, Node, Python), project-
# specific deps — is the workflow's job, installed via setup-* actions.
#
# Idempotent: every step checks existing state first.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# ---------- Visual Studio 2022 Build Tools (~6 GB, ~10 min) ----------
if (-not (Test-Path 'C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools')) {
    Write-Host "Installing Visual Studio 2022 Build Tools..."
    $url = 'https://aka.ms/vs/17/release/vs_BuildTools.exe'
    $dst = "$env:TEMP\vs_BuildTools.exe"
    Invoke-WebRequest -Uri $url -OutFile $dst
    $vsArgs = @(
        '--quiet','--wait','--norestart','--nocache'
        '--add','Microsoft.VisualStudio.Workload.VCTools'
        '--add','Microsoft.VisualStudio.Component.VC.Tools.x86.x64'
        '--add','Microsoft.VisualStudio.Component.VC.Tools.ARM64'
        '--add','Microsoft.VisualStudio.Component.Windows11SDK.22621'
    )
    $vsOut = "$env:TEMP\vs-install.out.log"
    $vsErr = "$env:TEMP\vs-install.err.log"
    $p = Start-Process -FilePath $dst -ArgumentList $vsArgs -Wait -PassThru -NoNewWindow `
        -RedirectStandardOutput $vsOut -RedirectStandardError $vsErr
    if ($p.ExitCode -ne 0 -and $p.ExitCode -ne 3010) {
        $tail = if (Test-Path $vsOut) { Get-Content $vsOut -Tail 20 -ErrorAction SilentlyContinue } else { "(no log)" }
        throw "VS Build Tools install failed: exit $($p.ExitCode). Tail:`n$tail"
    }
    Write-Host "VS Build Tools installed"
}

# ---------- Chocolatey ----------
if (-not (Get-Command choco -ErrorAction SilentlyContinue)) {
    Write-Host "Installing Chocolatey..."
    Set-ExecutionPolicy Bypass -Scope Process -Force
    [System.Net.ServicePointManager]::SecurityProtocol = `
        [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
    Invoke-Expression ((New-Object System.Net.WebClient).DownloadString(
        'https://community.chocolatey.org/install.ps1'))
    # Add to machine PATH so subsequent processes see it.
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    if ($machinePath -notlike '*chocolatey\bin*') {
        [Environment]::SetEnvironmentVariable('Path',
            "$machinePath;C:\ProgramData\chocolatey\bin", 'Machine')
    }
}

Write-Host "krapow toolchain installed"
