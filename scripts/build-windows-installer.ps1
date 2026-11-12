param(
    [Parameter(Mandatory)][string]$ClientBundle,
    [Parameter(Mandatory)][string]$Cli,
    [Parameter(Mandatory)][string]$Version,
    [Parameter(Mandatory)][string]$OutputDir,
    [string]$Server = 'https://api.endlessnet.ru',
    [switch]$Sign,
    [string]$CertificateThumbprint,
    [string]$SignTool = 'signtool.exe'
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
if ($Version -cnotmatch '^v?0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') { throw 'Expected v0.MINOR.PATCH' }
$ClientBundle = (Resolve-Path -LiteralPath $ClientBundle).Path
$Cli = (Resolve-Path -LiteralPath $Cli).Path
$output = [IO.Path]::GetFullPath($OutputDir)
if ($output.Equals($ClientBundle, [StringComparison]::OrdinalIgnoreCase) -or
    $output.StartsWith($ClientBundle + '\', [StringComparison]::OrdinalIgnoreCase) -or
    $Cli.StartsWith($output + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw 'OutputDir must be outside the input bundle and CLI directory'
}
New-Item -ItemType Directory -Force -Path $output | Out-Null
$work = Join-Path ([IO.Path]::GetTempPath()) ('endlessnet-client-msi-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $work | Out-Null
try {
    foreach ($file in (Join-Path $ClientBundle 'client.exe'), (Join-Path $ClientBundle 'wintun.dll'), $Cli) {
        $stream = [IO.File]::OpenRead($file)
        try {
            $reader = [IO.BinaryReader]::new($stream)
            if ($reader.ReadUInt16() -ne 0x5A4D) { throw "Not a PE image: $file" }
            $stream.Position = 0x3C
            $stream.Position = $reader.ReadUInt32()
            if ($reader.ReadUInt32() -ne 0x4550 -or $reader.ReadUInt16() -ne 0x8664) { throw "Expected Windows x64 image: $file" }
        } finally { $stream.Dispose() }
    }
    # Exact producer contract pairing is mandatory before validating/packaging.
    # These pure modes perform no SCM/state I/O and require no service settings.
    $serviceDigest = @(& (Join-Path $ClientBundle 'client.exe') print-contract 2>$null)
    if ($LASTEXITCODE -ne 0 -or $serviceDigest.Count -ne 1 -or
        $serviceDigest[0] -cnotmatch '^[0-9a-f]{64}$') { throw 'Client does not expose a canonical contract digest' }
    $cliDigest = @(& $Cli --print-contract 2>$null)
    if ($LASTEXITCODE -ne 0 -or $cliDigest.Count -ne 1 -or
        $cliDigest[0] -cnotmatch '^[0-9a-f]{64}$' -or $cliDigest[0] -cne $serviceDigest[0]) {
        throw 'Client and CLI must use the same exact Client API contract'
    }
    $defaultsPath = Join-Path $ClientBundle 'install-defaults.json'
    $defaults = Get-Content -LiteralPath $defaultsPath -Raw | ConvertFrom-Json
    $addresses = @(Get-NetIPConfiguration | Where-Object { $_.IPv4DefaultGateway } | ForEach-Object { $_.IPv4Address.IPAddress } | Select-Object -Unique)
    if ($addresses.Count -ne 1) { throw 'Cannot select one active IPv4 interface for configuration validation' }
    $endpoint = $addresses[0] + ':' + $defaults.ENDLESSNET_CLIENT_CONNECTION_LISTEN_PORT
    $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $data = Join-Path ([Environment]::GetFolderPath('CommonApplicationData')) 'EndlessNet\Client'
    $profile = Join-Path $work 'service.json'
    & python -B (Join-Path $PSScriptRoot 'windows_installer.py') configure --defaults $defaultsPath --server $Server --endpoint $endpoint --sid $sid --state-directory (Join-Path $data 'state') --output $profile
    if ($LASTEXITCODE) { throw 'Cannot prepare Client installation settings' }
    $settings = Get-Content -LiteralPath $profile -Raw | ConvertFrom-Json
    $process = [Diagnostics.ProcessStartInfo]::new((Join-Path $ClientBundle 'client.exe'), 'validate-config')
    $process.UseShellExecute = $false
    $process.CreateNoWindow = $true
    foreach ($key in @($process.EnvironmentVariables.Keys)) {
        if ($key.StartsWith('ENDLESSNET_CLIENT_', [StringComparison]::OrdinalIgnoreCase)) { $process.EnvironmentVariables.Remove($key) }
    }
    foreach ($entry in $settings.PSObject.Properties) { $process.EnvironmentVariables[$entry.Name] = $entry.Value }
    $process.EnvironmentVariables['ENDLESSNET_CLIENT_RUNTIME_INSTANCE_FILE'] = Join-Path $data 'runtime-instance'
    $child = [Diagnostics.Process]::Start($process)
    try { $child.WaitForExit(); if ($child.ExitCode -ne 0) { throw 'Client rejected installation settings' } }
    finally { $child.Dispose() }

    & cmake -S (Join-Path $repo 'packaging/windows') -B (Join-Path $work 'native') -A x64
    if ($LASTEXITCODE) { throw 'Native installer configure failed' }
    & cmake --build (Join-Path $work 'native') --config Release
    if ($LASTEXITCODE) { throw 'Native installer build failed' }
    $actions = Join-Path $work 'native/Release/endlessnet_client_installer_actions.dll'
    & python -B (Join-Path $PSScriptRoot 'windows_installer.py') render --bundle $ClientBundle --cli $Cli --service-profile $profile --actions $actions --version $Version --output $work
    if ($LASTEXITCODE) { throw 'Client MSI source generation failed' }
    $msi = Join-Path $output ('EndlessNetClient-v' + $Version.TrimStart('v') + '-windows-amd64.msi')
    & wix build (Join-Path $work 'EndlessNetClient.wxs') -arch x64 -o $msi
    if ($LASTEXITCODE) { throw 'Client MSI compilation failed' }
    if ($Sign) {
        if (-not $CertificateThumbprint) { throw 'CertificateThumbprint is required for signing' }
        & $SignTool sign /sha1 $CertificateThumbprint /fd SHA256 $msi
        if ($LASTEXITCODE) { throw 'MSI signing failed' }
    } else { Write-Warning 'Unsigned development MSI; signed server bootstrap and end-to-end connection are not supplied by packaging.' }
    Get-FileHash -Algorithm SHA256 -LiteralPath $msi
} finally {
    $resolved = [IO.Path]::GetFullPath($work)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    if ($resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path $resolved -Leaf).StartsWith('endlessnet-client-msi-')) {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
