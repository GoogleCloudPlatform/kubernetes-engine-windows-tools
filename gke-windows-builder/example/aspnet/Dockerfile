# escape=`

ARG WINDOWS_VERSION=20H2
FROM mcr.microsoft.com/dotnet/framework/aspnet:4.8-windowsservercore-$WINDOWS_VERSION
# Set shell to powershell
SHELL ["powershell", "-Command", "$ProgressPreference = 'SilentlyContinue'; $VerbosePreference = 'Continue'; $ErrorActionPreference = 'Stop';"]
# Copy runtime M4A utilities
COPY "m4a" "C:\m4a"
# Enabling extra Windows features
RUN Enable-WindowsOptionalFeature -Online -All -FeatureName IIS-WebServerManagementTools

# Installing extra .NET Dlls from GAC
COPY "gac" "m4a\gac"
RUN C:\m4a\install_gac.ps1 -GacDir C:\m4a\gac
# Setting up IIS
# Copying IIS files
COPY ["c\\inetpub\\wwwroot", "c:\\inetpub\\wwwroot"]
COPY ["c\\aspnet", "c:\\aspnet"]

# Setting up IIS
RUN c:\windows\system32\inetsrv\appcmd.exe set config \"/section:httpLogging\" \"/dontLog:False\"

# Removing default IIS Website
RUN Remove-Website -Name 'Default Web Site'
# Creating IIS AppPools
RUN c:\windows\system32\inetsrv\appcmd.exe set apppool \"DefaultAppPool\" \"/processModel.identityType:ApplicationPoolIdentity\"; `
c:\windows\system32\inetsrv\appcmd.exe set apppool \".NET v4.5 Classic\" \"/processModel.identityType:ApplicationPoolIdentity\"; `
c:\windows\system32\inetsrv\appcmd.exe set apppool \".NET v4.5\" \"/processModel.identityType:ApplicationPoolIdentity\";

# Adding website Default Web Site
RUN New-Website -Name 'Default Web Site' -PhysicalPath 'c:\inetpub\wwwroot' -Port 80 -Force; `
c:\windows\system32\inetsrv\appcmd.exe set site \"Default Web Site\" \"/[path='/'].applicationPool:DefaultAppPool\";

# Adding website AspNet
RUN New-Website -Name 'AspNet' -PhysicalPath 'c:\aspnet' -Port 8080 -Force; `
c:\windows\system32\inetsrv\appcmd.exe set site \"AspNet\" \"/[path='/'].applicationPool:DefaultAppPool\";

# Applying ACLs
COPY set_acls.bat C:\set_acls.bat
RUN cmd /c C:\set_acls.bat
# M4A Entrypoint wrapper
SHELL ["cmd", "/S", "/C" ]
ENTRYPOINT powershell -Command C:\m4a\entrypoint.ps1
