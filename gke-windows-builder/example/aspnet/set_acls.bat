


icacls "c:\inetpub\wwwroot" /grant "NT AUTHORITY\IUSR":(OI)(CI)(RX)
icacls "c:\inetpub\wwwroot" /grant "BUILTIN\IIS_IUSRS":(OI)(CI)(RX)
icacls "c:\inetpub\wwwroot" /grant "IIS APPPOOL\DefaultAppPool":(OI)(CI)(RX)
icacls "c:\inetpub\wwwroot" /grant "BUILTIN\IIS_IUSRS":(RX)
icacls "c:\inetpub\wwwroot" /grant "BUILTIN\IIS_IUSRS":(S)
icacls "c:\inetpub\wwwroot" /grant "BUILTIN\IIS_IUSRS":(CI)(OI)(S)



icacls "c:\aspnet" /grant "NT AUTHORITY\IUSR":(OI)(CI)(RX)
icacls "c:\aspnet" /grant "BUILTIN\IIS_IUSRS":(OI)(CI)(RX)
icacls "c:\aspnet" /grant "IIS APPPOOL\DefaultAppPool":(OI)(CI)(RX)
