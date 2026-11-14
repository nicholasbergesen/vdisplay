# Registers vdisplay as a logon task for the current user, run with highest privileges, and starts it.
# A second trigger every 5 minutes restarts it if it has stopped (an instance already running is left alone).
#   powershell -NoProfile -ExecutionPolicy Bypass -File register-task.ps1 [-Exe <path>] [-Width 1920] [-Height 1080] [-Log <file>] [-TaskName vdisplay] [-TaskPath \]
param(
    [string]$Exe = (Join-Path $PSScriptRoot 'vdisplay.exe'),
    [int]$Width = 1920,
    [int]$Height = 1080,
    [string]$Log = (Join-Path $env:LOCALAPPDATA 'vdisplay\vdisplay.log'),
    [string]$TaskName = 'vdisplay',
    [string]$TaskPath = ''
)
$ErrorActionPreference = 'Stop'
if (-not (Test-Path $Exe)) { throw "no vdisplay.exe at $Exe; build it with: go build -o vdisplay.exe ." }
New-Item -ItemType Directory -Force (Split-Path $Log) | Out-Null
$user = "$env:USERDOMAIN\$env:USERNAME"
$action = New-ScheduledTaskAction -Execute $Exe -Argument "-w $Width -h $Height -log `"$Log`""
$atLogon = New-ScheduledTaskTrigger -AtLogOn -User $user
$every5 = New-ScheduledTaskTrigger -Once -At (Get-Date).Date -RepetitionInterval (New-TimeSpan -Minutes 5)
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskPath $TaskPath -TaskName $TaskName -Action $action -Trigger @($atLogon, $every5) -Principal $principal -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskPath $TaskPath -TaskName $TaskName
"registered and started task '$TaskName'; log: $Log"
