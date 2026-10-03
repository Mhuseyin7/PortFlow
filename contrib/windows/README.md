# Windows Service for PortFlow

PortFlow does not ship a native Windows Service wrapper — NSSM (the
"non-sucking service manager") is the easiest way to run any plain
executable as a service.

```powershell
# From an elevated PowerShell
choco install nssm                                # or: scoop install nssm
.\install-service.ps1 -BinaryPath "C:\Program Files\PortFlow\portflow.exe"
```

The script:
- Registers `PortFlow` as an auto-start Windows service via NSSM
- Writes stdout/stderr to `%ProgramData%\PortFlow\logs\`
- Starts the service

Verify with:

```powershell
Get-Service PortFlow
Get-Content "$env:ProgramData\PortFlow\logs\portflow.err.log" -Wait
```

Remove with:

```powershell
nssm remove PortFlow confirm
```
