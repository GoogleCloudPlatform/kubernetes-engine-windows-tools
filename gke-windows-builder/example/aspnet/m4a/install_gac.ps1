param ([string]$gacDir)

$ErrorActionPreference = 'Continue'

[System.Reflection.Assembly]::Load("System.EnterpriseServices, Version=4.0.0.0, Culture=neutral, PublicKeyToken=b03f5f7f11d50a3a")

$publish = New-Object System.EnterpriseServices.Internal.Publish


$dlls = Get-ChildItem $gacDir -Filter "*.dll" -File -Recurse
$gacDirLength = $gacDir.Length
foreach ($dll in $dlls) {
	Write-Output "Installing " + $dll.FullName
	$publish.GacInstall($dll.FullName)
	$dllDir = Split-Path -Path $dll.FullName -Parent
	$targetDir = "C:\\Windows\\" + $dll.FullName.Substring($gacDirLength)
	if (Test-Path $targetDir) {
		$otherFiles = Get-ChildItem $dllDir -Exclude "*.dll"
	} else {
		Write-Output "Gac-Install didn't work for " + $dll.FullName + " -- manually copying"
		New-Item $targetDir -itemtype Directory | Out-Null
		$otherFiles = Get-ChildItem $dllDir
	}
	foreach ($otherFile in $otherFiles) {
	    Write-Output "Copying resources: " + $otherFile.FullName
	    Copy-Item $otherFile.FullName -Destination $targetDir
	}
}
