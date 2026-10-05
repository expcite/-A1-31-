<#
.SYNOPSIS
  M2.1 end-to-end acceptance harness for Windows.

.DESCRIPTION
  Applies EXACTLY the same acceptance rules as scripts/m2-e2e.sh by delegating
  to the shared verifier scripts/verify_e2e_events.py. The M2 PowerShell script
  had its own weaker checks; keeping one verifier means the two platforms cannot
  drift apart.

  Every run gets its own directory under results\raw\e2e\<run_id>\ containing
  RUNNING, then PASS / FAIL / INTERRUPTED, plus both event streams and the
  verifier result. A failed run therefore cannot leave a stale PASS behind.

  This is an END-TO-END FUNCTIONAL check. It is NOT a benchmark.

.PARAMETER OutputRoot
  Where run directories are created. Default: results\raw\e2e

.PARAMETER Go
  Path to the go tool if it is not on PATH.

.PARAMETER Python
  Interpreter for the shared verifier. Default: python

.PARAMETER Samples
  Telemetry samples. Default: 3

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\m2-e2e.ps1
#>
[CmdletBinding()]
param(
  [string]$OutputRoot = '',
  [string]$Go = 'go',
  [string]$Python = 'python',
  [int]$Samples = 3
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if ($OutputRoot -eq '') { $OutputRoot = Join-Path $root 'results\raw\e2e' }

$runId = 'm21-' + (Get-Date -Format 'yyyyMMddTHHmmssZ') + '-' + $PID
$work = Join-Path ([System.IO.Path]::GetTempPath()) ('edgehybrid-e2e-' + [Guid]::NewGuid().ToString('N'))
$bin = Join-Path $work 'bin'
$runtime = Join-Path $work 'runtime'
$runDir = Join-Path $OutputRoot $runId

$gwProc = $null
$status = 'INTERRUPTED'
$gwCode = 'not-run'
$devCode = 'not-run'
$verifyCode = 'not-run'
$exitCode = 1

function Write-Step([string]$text) { Write-Host "== $text" }
function Fail([string]$text) { throw [System.Exception]::new($text) }

# Reads a JSON-lines file into objects. Empty array when absent.
function Read-Events([string]$path) {
  if (-not (Test-Path -LiteralPath $path)) { return @() }
  $out = New-Object System.Collections.ArrayList
  foreach ($line in (Get-Content -LiteralPath $path)) {
    if ([string]::IsNullOrWhiteSpace($line)) { continue }
    [void]$out.Add(($line | ConvertFrom-Json))
  }
  return $out.ToArray()
}

# Safe access to an optional JSON property: events omit empty fields and
# Set-StrictMode turns a missing property into an error.
function Get-Field($obj, [string]$name) {
  if ($null -eq $obj) { return '' }
  $p = $obj.PSObject.Properties[$name]
  if ($null -eq $p -or $null -eq $p.Value) { return '' }
  return $p.Value
}

try {
  New-Item -ItemType Directory -Force -Path $bin, $runtime, $runDir | Out-Null
  Set-Content -LiteralPath (Join-Path $runDir 'result') -Value 'RUNNING' -Encoding ascii
  Set-Content -LiteralPath (Join-Path $runDir 'status') -Value @("status=RUNNING", "run_id=$runId") -Encoding ascii

  Write-Host "== run_id: $runId"
  Write-Host "== output: $runDir"

  Write-Step '1. build'
  Push-Location $root
  try {
    & $Go build -o "$bin\" ./cmd/...
    if ($LASTEXITCODE -ne 0) { Fail 'go build failed' }
  } finally { Pop-Location }

  $secctl = Join-Path $bin 'secctl.exe'
  $gateway = Join-Path $bin 'gateway.exe'
  $device = Join-Path $bin 'device.exe'

  Write-Step '2. create fresh test identities'
  & $secctl init-demo -dir $runtime | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'init-demo failed' }

  $gwConfig = Join-Path $work 'gateway.json'
  $devConfig = Join-Path $work 'device.json'
  $gwEvents = Join-Path $runDir 'gateway.jsonl'
  $devEvents = Join-Path $runDir 'device.jsonl'

  Write-Step '3. write the gateway configuration'
  $gwJson = @"
{
  "role": "gateway",
  "id": "GW001",
  "address": "127.0.0.1:0",
  "minimum_mode": "traditional",
  "allow_legacy": true,
  "supported_suites": [1],
  "handshake_timeout_ms": 5000,
  "max_frame_bytes": 16384,
  "trust_store": "$runtime\trust.json",
  "key_directory": "$runtime\keys"
}
"@
  Set-Content -LiteralPath $gwConfig -Value $gwJson -Encoding ascii

  Write-Step '4. start the gateway'
  $gwProc = Start-Process -FilePath $gateway -PassThru -NoNewWindow `
    -ArgumentList @('-config', $gwConfig, '-max-sessions', '1', '-run-for', '60s',
                    '-run-id', $runId, '-events', $gwEvents) `
    -RedirectStandardOutput (Join-Path $runDir 'gateway.stdout') `
    -RedirectStandardError (Join-Path $runDir 'gateway.stderr')

  # Readiness comes from the gateway's OWN event, never from a fixed sleep.
  $address = ''
  for ($i = 0; $i -lt 200; $i++) {
    foreach ($e in (Read-Events $gwEvents)) {
      if ((Get-Field $e 'stage') -eq 'listen' -and (Get-Field $e 'outcome') -eq 'ready') {
        $address = Get-Field $e 'detail'
        break
      }
    }
    if ($address -ne '') { break }
    if ($gwProc.HasExited) {
      $err = Get-Content -LiteralPath (Join-Path $runDir 'gateway.stderr') -Raw
      Fail "gateway did not start: $err"
    }
    Start-Sleep -Milliseconds 100
  }
  if ($address -eq '') { Fail 'the gateway never reported a listening address' }
  Write-Host "   gateway is listening on $address"

  Write-Step '5. write the device configuration and run the device'
  $devJson = @"
{
  "role": "device",
  "id": "D001",
  "address": "$address",
  "minimum_mode": "traditional",
  "allow_legacy": true,
  "supported_suites": [1],
  "handshake_timeout_ms": 5000,
  "max_frame_bytes": 16384,
  "trust_store": "$runtime\trust.json",
  "key_directory": "$runtime\keys",
  "expected_peer_id": "GW001"
}
"@
  Set-Content -LiteralPath $devConfig -Value $devJson -Encoding ascii

  & $device -config $devConfig -samples $Samples -run-for 30s -run-id $runId -events $devEvents
  $devCode = $LASTEXITCODE
  if ($devCode -ne 0) { Fail "the device run failed with exit code $devCode" }

  Write-Step '6. wait for the gateway'
  $gwProc.WaitForExit()
  $gwCode = $gwProc.ExitCode
  if ($gwCode -ne 0) { Fail "the gateway exited with code $gwCode" }
  $gwProc = $null

  Write-Step '7. verify the events (shared strict verifier)'
  $verifier = Join-Path $root 'scripts\verify_e2e_events.py'
  & $Python $verifier `
    --gateway $gwEvents --device $devEvents `
    --run-id $runId --samples $Samples `
    --device-id D001 --gateway-id GW001 --expect-suite 1 `
    --out (Join-Path $runDir 'verify.json')
  $verifyCode = $LASTEXITCODE
  if ($verifyCode -ne 0) { Fail "the shared event verifier rejected this run (exit $verifyCode)" }

  # Convenience copies under the M2 file names: written only on success and
  # always from THIS run, so they cannot be a stale PASS.
  Copy-Item -LiteralPath $gwEvents -Destination (Join-Path $root 'results\raw\m2_e2e_gateway.jsonl') -Force
  Copy-Item -LiteralPath $devEvents -Destination (Join-Path $root 'results\raw\m2_e2e_device.jsonl') -Force
  Get-Content -LiteralPath $gwEvents, $devEvents |
    Set-Content -LiteralPath (Join-Path $root 'results\raw\m2_e2e_events.jsonl') -Encoding ascii
  Copy-Item -LiteralPath (Join-Path $runDir 'verify.json') `
            -Destination (Join-Path $root 'results\raw\m2_e2e_summary.json') -Force

  $status = 'PASS'
  $exitCode = 0
  Write-Host ''
  Write-Host 'PASS: real TCP handshake, telemetry, control and acknowledgement verified on both sides.'
  Write-Host "      Event streams and the verifier result are in $runDir"
} catch {
  $status = 'FAIL'
  $exitCode = 1
  Write-Error ("FAIL: " + $_.Exception.Message)
  try {
    Add-Content -LiteralPath (Join-Path $runDir 'diagnostics.txt') -Value ("FAIL: " + $_.Exception.Message)
  } catch { }
} finally {
  if ($null -ne $gwProc -and -not $gwProc.HasExited) {
    try { $gwProc.Kill(); $gwCode = 'killed' } catch { }
  }
  # The status file is written LAST and holds only THIS run's result.
  try {
    Set-Content -LiteralPath (Join-Path $runDir 'result') -Value $status -Encoding ascii
    Set-Content -LiteralPath (Join-Path $runDir 'status') -Encoding ascii -Value @(
      "status=$status",
      "run_id=$runId",
      "gateway_exit_code=$gwCode",
      "device_exit_code=$devCode",
      "verifier_exit_code=$verifyCode",
      "script_exit_code=$exitCode",
      "samples=$Samples"
    )
  } catch { }
  try { Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue } catch { }
}

exit $exitCode
