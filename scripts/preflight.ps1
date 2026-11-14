#Requires -Version 7.0
param([switch]$RequireDesktop)
$Profile = if ($RequireDesktop) { 'Desktop' } else { 'Frontend' }
& (Join-Path $PSScriptRoot 'doctor.ps1') -Profile $Profile
