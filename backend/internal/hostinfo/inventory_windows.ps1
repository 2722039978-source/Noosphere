$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false)
$issues=[System.Collections.Generic.List[object]]::new()
function Issue([string]$component) { $issues.Add(@{component=$component;code='QUERY_UNAVAILABLE';message='component unavailable'}) }
$system=@{}; $board=@{}; $osInfo=@{}; $cpus=@(); $modules=@(); $gpus=@(); $disks=@(); $operatingSystem=$null
try { $x=Get-CimInstance Win32_ComputerSystem; $system=@{manufacturer=[string]$x.Manufacturer;model=[string]$x.Model} } catch { Issue 'system' }
try { $x=Get-CimInstance Win32_BaseBoard | Select-Object -First 1; $board=@{manufacturer=[string]$x.Manufacturer;model=[string]$x.Product} } catch { Issue 'motherboard' }
try { $operatingSystem=Get-CimInstance Win32_OperatingSystem; $osInfo=@{name=[string]$operatingSystem.Caption;version=[string]$operatingSystem.Version;build=[string]$operatingSystem.BuildNumber;architecture=[string]$operatingSystem.OSArchitecture} } catch { Issue 'os' }
try { $cpus=@(Get-CimInstance Win32_Processor | ForEach-Object { @{manufacturer=[string]$_.Manufacturer;model=[string]$_.Name;physical_cores=[int]$_.NumberOfCores;logical_cores=[int]$_.NumberOfLogicalProcessors;max_clock_mhz=[int]$_.MaxClockSpeed} }) } catch { Issue 'cpu' }
try { $modules=@(Get-CimInstance Win32_PhysicalMemory | ForEach-Object { $cap=$null; $speed=$null; if ($_.Capacity -gt 0) {$cap=[UInt64]$_.Capacity}; if ($_.ConfiguredClockSpeed -gt 0) {$speed=[UInt64]$_.ConfiguredClockSpeed}; @{manufacturer=[string]$_.Manufacturer;part_number=[string]$_.PartNumber;capacity_bytes=$cap;speed_mts=$speed} }) } catch { Issue 'memory' }
try {
 $gpus=@(Get-CimInstance Win32_VideoController | ForEach-Object {
  $gpu=$_; $total=$null; $vramSource='unavailable'
  try {
   # Device instance/driver keys are used for an exact local join, never emitted.
   $instance=Get-Item -LiteralPath ('Registry::HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Enum\'+$gpu.PNPDeviceID)
   $driver=[string]$instance.GetValue('Driver')
   if ($driver -match '^\{[A-Fa-f0-9-]{36}\}\\[0-9]{4}$') {
    $adapter=Get-Item -LiteralPath ('Registry::HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Class\'+$driver)
    $name='HardwareInformation.qwMemorySize'
    if ($adapter.GetValueKind($name) -eq [Microsoft.Win32.RegistryValueKind]::QWord) {
     $reported=$adapter.GetValue($name)
     if ($reported -gt 0) { $total=[UInt64]$reported; $vramSource='windows_registry_qword_pnp_driver' }
    }
   }
  } catch { }
  @{model=[string]$gpu.Name;manufacturer=[string]$gpu.AdapterCompatibility;driver_version=[string]$gpu.DriverVersion;total_vram_bytes=$total;vram_source=$vramSource}
 })
} catch { Issue 'gpu' }
try {
 if ($operatingSystem -and $operatingSystem.SystemDrive -match '^[A-Za-z]:$') {
  $logical=Get-CimInstance Win32_LogicalDisk -Filter ("DeviceID='"+$operatingSystem.SystemDrive+"'")
  $diskRecords=@($logical | Get-CimAssociatedInstance -Association Win32_LogicalDiskToPartition | Get-CimAssociatedInstance -Association Win32_DiskDriveToDiskPartition)
  $disks=@($diskRecords | Sort-Object Index -Unique | ForEach-Object { $size=$null; if($_.Size -gt 0){$size=[UInt64]$_.Size}; @{model=[string]$_.Model;capacity_bytes=$size;interface_type=[string]$_.InterfaceType} })
 }
} catch { Issue 'system_disk' }
@{system=$system;motherboard=$board;os=$osInfo;cpus=@($cpus);memory=@{modules=@($modules)};gpus=@($gpus);system_disks=@($disks);issues=@($issues.ToArray())} | ConvertTo-Json -Depth 8 -Compress
