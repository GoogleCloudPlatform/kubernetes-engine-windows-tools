function RunProcess
{
    Param (
         [Parameter(Mandatory=$true)]$FileName,
         $Args
    )

    $process = New-Object System.Diagnostics.Process
    $process.StartInfo.UseShellExecute = $false
    $process.StartInfo.RedirectStandardOutput = $true
    $process.StartInfo.RedirectStandardError = $true
    $process.StartInfo.FileName = $FileName
    if($Args) { $process.StartInfo.Arguments = $Args }
    $out = $process.Start()

    $StandardError = $process.StandardError.ReadToEnd()
    $StandardOutput = $process.StandardOutput.ReadToEnd()
    $process.WaitForExit()
    $ExitCode = $process.ExitCode

    $output = New-Object PSObject
    $output | Add-Member -type NoteProperty -name StandardOutput -Value $StandardOutput
    $output | Add-Member -type NoteProperty -name StandardError -Value $StandardError
    $output | Add-Member -type NoteProperty -name ExitCode -Value $ExitCode
    return $output
}

Write-Output "Migrate for Anthos (Windows)"

$certsYamlPath = [System.Environment]::GetEnvironmentVariable("M4A_CERT_YAML")
$certInstallScript = "c:\m4a\install_certs.ps1"
if (([string]::IsNullOrEmpty($certsYamlPath))) {
    $certsYamlPath = "C:\m4a\certs.yaml"
}

if ( Test-Path $certsYamlPath -PathType Leaf ) {
    $output = RunProcess -FileName "C:\m4a\ssltool.exe" -Args "${certsYamlPath} ${certInstallScript}"
    if ($output.ExitCode -ne 0) {
        Write-Output "Failed to build cert install script ${output.ExitCode}, stdout: ${output.StandardOutput} stderr: ${output.StandardError}"
    } else {
      Write-Output "Running SSL Cert Install script ${certInstallScript}"
      powershell.exe -File $certInstallScript
    }
}

. "C:\m4a\ssltools.ps1"
Set-Missing-Certificates

C:\m4a\LogMonitor.exe /Config C:\m4a\LogMonitorConfig.json powershell -Command C:\m4a\runiis.ps1

