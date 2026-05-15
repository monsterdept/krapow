$ErrorActionPreference = 'Stop'
# Silence the per-iteration progress objects that Invoke-WebRequest and
# Expand-Archive otherwise stream back as raw CLIXML over the SSH session.
$ProgressPreference = 'SilentlyContinue'
$dir = 'C:\actions-runner'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Set-Location $dir
$rel = Invoke-RestMethod 'https://api.github.com/repos/actions/runner/releases/latest'
$ver = $rel.tag_name.TrimStart('v')
$url = "https://github.com/actions/runner/releases/download/v$ver/actions-runner-win-x64-$ver.zip"
Invoke-WebRequest -Uri $url -OutFile runner.zip
Expand-Archive -Force -Path runner.zip -DestinationPath $dir
.\config.cmd --unattended --replace `
  --url '{{.RepoURL}}' --token '{{.RegToken}}' `
  --name '{{.Name}}' --labels '{{.Labels}}' --runasservice
