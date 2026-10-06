param(
  [string]$GoExe = '',
  [string]$PostgresBin = 'C:\Program Files\PostgreSQL\17\bin',
  [string]$RuntimeDir = '',
  [int]$DatabasePort = 55432
)
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (!$GoExe) {
  $installedGo = Get-Command go -ErrorAction SilentlyContinue
  $portableGo = [IO.Path]::GetFullPath((Join-Path $projectRoot '..\..\work\toolchain\go\bin\go.exe'))
  if ($installedGo) { $GoExe = $installedGo.Source }
  elseif (Test-Path -LiteralPath $portableGo) { $GoExe = $portableGo }
  else { throw 'Instale Go 1.27 ou informe -GoExe com o caminho do executável.' }
}
if (!$RuntimeDir) { $RuntimeDir = Join-Path $projectRoot '..\..\work\mezo-runtime' }
$RuntimeDir = [IO.Path]::GetFullPath($RuntimeDir)
$clusterDir = Join-Path $RuntimeDir 'postgres'
New-Item -ItemType Directory -Force -Path $RuntimeDir | Out-Null
function New-MezoSecret {
  $bytes = New-Object byte[] 32
  $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
  $rng.GetBytes($bytes)
  $rng.Dispose()
  return [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
}
$envFile = Join-Path $projectRoot '.env'
if (!(Test-Path -LiteralPath $envFile)) {
  $dbPassword = New-MezoSecret
  $adminPassword = New-MezoSecret
  $staffPassword = New-MezoSecret
  $secret = New-MezoSecret
  $content = @"
DATABASE_ADMIN_URL=postgres://mezo_provisioner:$dbPassword@127.0.0.1:$DatabasePort/postgres?sslmode=disable
APP_SECRET=$secret
ADMIN_EMAIL=admin@mezo.local
ADMIN_PASSWORD=$adminPassword
LISTEN_ADDR=127.0.0.1:4173
APP_ORIGIN=http://127.0.0.1:4173
SEED_DEMO=true
DEMO_STAFF_PASSWORD=$staffPassword
"@
  [IO.File]::WriteAllText($envFile, $content, [Text.UTF8Encoding]::new($false))
  $access = @"
# Acessos locais do Mezô

Administrador: admin@mezo.local
Senha: $adminPassword
Painel: http://127.0.0.1:4173/admin

Equipe Casa do Sol: ana@mezo.local
Senha: $staffPassword
Equipe: http://127.0.0.1:4173/r/casa-do-sol/equipe

Os links das mesas e os QR codes ficam no painel administrativo.
Este arquivo e .env são locais e excluídos do Git e da publicação.
"@
  [IO.File]::WriteAllText((Join-Path $projectRoot 'LOCAL-ACCESS.md'), $access, [Text.UTF8Encoding]::new($false))
}
if (!(Test-Path -LiteralPath (Join-Path $clusterDir 'PG_VERSION'))) {
  $match = [regex]::Match([IO.File]::ReadAllText($envFile), 'postgres://mezo_provisioner:([a-f0-9]+)@')
  if (!$match.Success) { throw 'A configuração .env não corresponde ao cluster local. Use PostgreSQL externo ou uma pasta de execução nova.' }
  $passwordFile = Join-Path $RuntimeDir 'init-password.txt'
  [IO.File]::WriteAllText($passwordFile, $match.Groups[1].Value, [Text.UTF8Encoding]::new($false))
  try {
    & (Join-Path $PostgresBin 'initdb.exe') -D $clusterDir -U mezo_provisioner --auth=scram-sha-256 --pwfile=$passwordFile --encoding=UTF8 --locale=C
    if ($LASTEXITCODE -ne 0) { throw 'Falha ao inicializar PostgreSQL local' }
  } finally { Remove-Item -LiteralPath $passwordFile -ErrorAction SilentlyContinue }
}
& (Join-Path $PostgresBin 'pg_ctl.exe') -D $clusterDir status 2>$null
if ($LASTEXITCODE -ne 0) {
  & (Join-Path $PostgresBin 'pg_ctl.exe') -D $clusterDir -l (Join-Path $RuntimeDir 'postgres.log') -o "-h 127.0.0.1 -p $DatabasePort" -w start
  if ($LASTEXITCODE -ne 0) { throw 'Falha ao iniciar PostgreSQL local' }
}
$pidFile = Join-Path $RuntimeDir 'app.pid'
$binary = Join-Path $RuntimeDir 'mezo.exe'
if (Test-Path -LiteralPath $pidFile) {
  $appProcessId = [int]([IO.File]::ReadAllText($pidFile).Trim())
  $oldProcess = Get-Process -Id $appProcessId -ErrorAction SilentlyContinue
  if ($oldProcess -and $oldProcess.Path -eq $binary) { Stop-Process -Id $appProcessId; $oldProcess.WaitForExit() }
}
$env:GOPATH = Join-Path $RuntimeDir '..\go-cache'
$env:GOCACHE = Join-Path $RuntimeDir '..\go-build-cache'
$env:GOTOOLCHAIN = 'local'
Push-Location $projectRoot
try {
  & $GoExe build -trimpath -o $binary .
  if ($LASTEXITCODE -ne 0) { throw 'Falha ao compilar Mezô' }
  $process = Start-Process -FilePath $binary -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $RuntimeDir 'app.log') -RedirectStandardError (Join-Path $RuntimeDir 'app-error.log')
  [IO.File]::WriteAllText($pidFile, [string]$process.Id)
} finally { Pop-Location }
Write-Output 'Mezô iniciado em http://127.0.0.1:4173. Acessos em LOCAL-ACCESS.md.'
