function Install-Certificate-And-Bind([string] $PfxPath, [string] $Password, [string] $SiteName, [int] $Port, [string] $Thumbprint) {
    $secure = ConvertTo-SecureString -AsPlainText -Force -String $Password
    Import-PfxCertificate -Password $secure -CertStoreLocation Cert:\LocalMachine\My -FilePath $PfxPath
    $binding = Get-WebBinding -Name $SiteName -Port $Port
    Write-Output "Installing Certificate for ${SiteName} from ${PfxPath}"
    $binding.AddSslCertificate($Thumbprint, "My")
}

function New-SelfSignedCert() {
    $cert = New-SelfSignedCertificate -DnsName "M4A.self.signed" -Friendlyname "M4A Self-Signed Cert" -CertStoreLocation Cert:\LocalMachine\My
    $rootStore = New-Object System.Security.Cryptography.X509Certificates.X509Store -ArgumentList Root, LocalMachine
    $rootStore.Open("MaxAllowed")
    $rootStore.Add($cert)
    $rootStore.Close()
    return $cert
}

function Set-Missing-Certificates() {
    $cert = New-SelfSignedCert
    $bindings = Get-WebBinding -Protocol "https"
    foreach ($bind in $bindings) {
        if (-not ([string]::IsNullOrEmpty($bind.certificateHash))) {
                continue
        }
	Write-Output "Installing Test Certificate for ${bind.ItemXPath}"
        $bind.AddSslCertificate($cert.Thumbprint, "My")
    }
}
