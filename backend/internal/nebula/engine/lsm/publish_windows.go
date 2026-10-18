//go:build windows

package lsm

import (
 "syscall"
 "unsafe"
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

// WRITE_THROUGH makes the name publication durable before WAL/old SST retirement.
func publishSST(source, target string) error {
 from,err:=syscall.UTF16PtrFromString(source);if err!=nil{return err}
 to,err:=syscall.UTF16PtrFromString(target);if err!=nil{return err}
 const moveFileWriteThrough = 0x8
 result,_,callErr:=moveFileEx.Call(uintptr(unsafe.Pointer(from)),uintptr(unsafe.Pointer(to)),moveFileWriteThrough)
 if result==0{return callErr};return nil
}
