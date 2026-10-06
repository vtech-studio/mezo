param([string]$RuntimeDir = '', [string]$PostgresBin = 'C:\Program Files\PostgreSQL\17\bin')
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (!$RuntimeDir) { $RuntimeDir = Join-Path $projectRoot '..\..\work\mezo-runtime' }
$RuntimeDir = [IO.Path]::GetFullPath($RuntimeDir)
$pidFile = Join-Path $RuntimeDir 'app.pid'
if (Test-Path -LiteralPath $pidFile) {
  $appProcessId = [int]([IO.File]::ReadAllText($pidFile).Trim())
  $process = Get-Process -Id $appProcessId -ErrorAction SilentlyContinue
  if ($process -and $process.Path -eq (Join-Path $RuntimeDir 'mezo.exe')) { Stop-Process -Id $appProcessId; $process.WaitForExit() }
}
$clusterDir = Join-Path $RuntimeDir 'postgres'
if (Test-Path -LiteralPath (Join-Path $clusterDir 'PG_VERSION')) {
  & (Join-Path $PostgresBin 'pg_ctl.exe') -D $clusterDir -m fast -w stop
}
