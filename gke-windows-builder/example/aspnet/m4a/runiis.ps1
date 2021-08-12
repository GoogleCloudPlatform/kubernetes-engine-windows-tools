Start-Service -Name 'W3SVC'
$iis = Get-Service 'W3SVC'
$iis.WaitForStatus('Running', '00:00:30')

while ($true) {
	Start-Sleep -Seconds 1
	$iis = Get-Service 'W3SVC'
	if ($iis.State -eq 'Stopped') {
		Write-Output 'IIS was stopped. Stopping container'
		Exit 0
	}
}
