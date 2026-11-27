//! A shared file lock is held by the gateway while its HTTP connection to
//! this model is open. A nonblocking exclusive lock prevents a model switch
//! from interrupting active generation, across the Go and Rust processes.
use std::fs::{self, File, OpenOptions};
use std::io;
use std::path::Path;

pub(super) fn exclusive(data: &Path) -> Result<File, String> {
    let directory = data.join("local-engines");
    fs::create_dir_all(&directory).map_err(|e| e.to_string())?;
    let file = OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(false)
        .open(directory.join("custom-requests.lock"))
        .map_err(|e| format!("无法打开模型请求锁：{e}"))?;
    try_lock(&file, true)
        .map_err(|e| format!("模型仍有活动请求或无法取得控制权。请先停止生成，再重试。({e})"))?;
    Ok(file)
}

#[cfg(unix)]
fn try_lock(file: &File, exclusive: bool) -> io::Result<()> {
    use std::os::fd::AsRawFd;
    let kind = if exclusive {
        libc::LOCK_EX
    } else {
        libc::LOCK_SH
    };
    if unsafe { libc::flock(file.as_raw_fd(), kind | libc::LOCK_NB) } != 0 {
        return Err(io::Error::last_os_error());
    }
    Ok(())
}

#[cfg(windows)]
fn try_lock(file: &File, exclusive: bool) -> io::Result<()> {
    use std::os::windows::io::AsRawHandle;
    use windows_sys::Win32::Storage::FileSystem::{
        LockFileEx, LOCKFILE_EXCLUSIVE_LOCK, LOCKFILE_FAIL_IMMEDIATELY,
    };
    let flags = LOCKFILE_FAIL_IMMEDIATELY
        | if exclusive {
            LOCKFILE_EXCLUSIVE_LOCK
        } else {
            0
        };
    // Lock byte zero, just like the Go transport. Closing the handle unlocks it.
    let mut overlapped = unsafe { std::mem::zeroed() };
    if unsafe { LockFileEx(file.as_raw_handle(), flags, 0, 1, 0, &mut overlapped) } == 0 {
        return Err(io::Error::last_os_error());
    }
    Ok(())
}

#[cfg(test)]
pub(super) fn reader(data: &Path) -> File {
    let file = File::open(data.join("local-engines/custom-requests.lock")).unwrap();
    try_lock(&file, false).unwrap();
    file
}
