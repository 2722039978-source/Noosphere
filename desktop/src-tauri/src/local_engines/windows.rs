use serde::{Deserialize, Serialize};
use std::mem::size_of;
use std::os::windows::io::{AsRawHandle, FromRawHandle, OwnedHandle};
use std::path::Path;
use std::ptr::{null, null_mut};
use windows_sys::Win32::Foundation::{FILETIME, WAIT_ABANDONED, WAIT_OBJECT_0};
use windows_sys::Win32::NetworkManagement::IpHelper::{
    GetExtendedTcpTable, MIB_TCPROW_OWNER_PID, TCP_TABLE_OWNER_PID_LISTENER,
};
use windows_sys::Win32::System::SystemInformation::{GlobalMemoryStatusEx, MEMORYSTATUSEX};
use windows_sys::Win32::System::Threading::{
    CreateMutexW, GetProcessTimes, OpenProcess, QueryFullProcessImageNameW, ReleaseMutex,
    WaitForSingleObject, PROCESS_QUERY_LIMITED_INFORMATION,
};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub(super) struct ListenerOwner {
    pid: u32,
    creation: u64,
}

impl ListenerOwner {
    pub(super) fn pid(self) -> u32 {
        self.pid
    }

    pub(super) fn is_lm_studio(self) -> bool {
        let process = unsafe { OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, self.pid) };
        if process.is_null() {
            return false;
        }
        let process = unsafe { OwnedHandle::from_raw_handle(process) };
        let mut path = vec![0u16; 32768];
        let mut length = path.len() as u32;
        if unsafe {
            QueryFullProcessImageNameW(process.as_raw_handle(), 0, path.as_mut_ptr(), &mut length)
        } == 0
        {
            return false;
        }
        let path = String::from_utf16_lossy(&path[..length as usize]);
        let name = Path::new(&path)
            .file_name()
            .and_then(|name| name.to_str())
            .unwrap_or_default()
            .to_ascii_lowercase();
        matches!(
            name.as_str(),
            "lm studio.exe" | "lmstudio.exe" | "llmster.exe" | "lms.exe"
        )
    }

    pub(super) fn observe(port: u16) -> Option<Self> {
        const AF_INET: u32 = 2;
        let mut length = 0u32;
        for _ in 0..3 {
            if length == 0 {
                // The kernel supplies the required size when the buffer is null.
                unsafe {
                    GetExtendedTcpTable(
                        null_mut(),
                        &mut length,
                        0,
                        AF_INET,
                        TCP_TABLE_OWNER_PID_LISTENER,
                        0,
                    )
                };
            }
            if !(4..=4 * 1024 * 1024).contains(&length) {
                return None;
            }
            let mut table = vec![0u32; (length as usize).div_ceil(size_of::<u32>())];
            let status = unsafe {
                GetExtendedTcpTable(
                    table.as_mut_ptr().cast(),
                    &mut length,
                    0,
                    AF_INET,
                    TCP_TABLE_OWNER_PID_LISTENER,
                    0,
                )
            };
            if status != 0 {
                continue;
            }
            let rows = table[0] as usize;
            if rows
                > (table.len() * size_of::<u32>() - size_of::<u32>())
                    / size_of::<MIB_TCPROW_OWNER_PID>()
            {
                return None;
            }
            // MIB_TCPTABLE_OWNER_PID starts with a DWORD count followed by
            // DWORD-aligned rows. Bound the slice by the successful buffer size.
            let rows = unsafe {
                std::slice::from_raw_parts(table[1..].as_ptr().cast::<MIB_TCPROW_OWNER_PID>(), rows)
            };
            let mut matches = rows.iter().filter(|row| {
                let local_port = u16::from_be(row.dwLocalPort as u16);
                let address = row.dwLocalAddr.to_ne_bytes();
                local_port == port && (address[0] == 127 || address == [0; 4])
            });
            let row = matches.next()?;
            if matches.next().is_some() {
                return None;
            }
            return Self::for_process(row.dwOwningPid);
        }
        None
    }

    fn for_process(pid: u32) -> Option<Self> {
        let process = unsafe { OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid) };
        if process.is_null() {
            return None;
        }
        let process = unsafe { OwnedHandle::from_raw_handle(process) };
        let mut times: [FILETIME; 4] = Default::default();
        let [creation, exit, kernel, user] = &mut times;
        let ok = unsafe { GetProcessTimes(process.as_raw_handle(), creation, exit, kernel, user) };
        if ok == 0 {
            return None;
        }
        Some(Self {
            pid,
            creation: (u64::from(creation.dwHighDateTime) << 32)
                | u64::from(creation.dwLowDateTime),
        })
    }
}

pub(super) struct PortableLock(OwnedHandle);

impl PortableLock {
    pub(super) fn acquire() -> Result<Self, String> {
        // Coordinate with the already deployed DeepSeek and Qwen scripts.
        let name: Vec<u16> = r"Global\NoosphereLocalModels-18787-18788-Control-v1"
            .encode_utf16()
            .chain([0])
            .collect();
        let handle = unsafe { CreateMutexW(null(), 0, name.as_ptr()) };
        if handle.is_null() {
            return Err("无法创建本地模型的启动协调锁。".to_string());
        }
        let handle = unsafe { OwnedHandle::from_raw_handle(handle) };
        let wait = unsafe { WaitForSingleObject(handle.as_raw_handle(), 1000) };
        if wait != WAIT_OBJECT_0 && wait != WAIT_ABANDONED {
            return Err("另一个本地模型正在启动或停止，请完成后重试。".to_string());
        }
        Ok(Self(handle))
    }
}

impl Drop for PortableLock {
    fn drop(&mut self) {
        unsafe { ReleaseMutex(self.0.as_raw_handle()) };
    }
}

pub(super) fn low_available_memory() -> bool {
    let mut memory = MEMORYSTATUSEX {
        dwLength: size_of::<MEMORYSTATUSEX>() as u32,
        ..Default::default()
    };
    unsafe { GlobalMemoryStatusEx(&mut memory) != 0 && memory.ullAvailPhys < 512 * 1024 * 1024 }
}
