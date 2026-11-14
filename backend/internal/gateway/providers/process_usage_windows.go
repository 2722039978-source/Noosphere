package providers

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	tcpTable      = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")
	processMemory = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	pdh           = windows.NewLazySystemDLL("pdh.dll")
	pdhOpen       = pdh.NewProc("PdhOpenQueryW")
	pdhClose      = pdh.NewProc("PdhCloseQuery")
	pdhAddEnglish = pdh.NewProc("PdhAddEnglishCounterW")
	pdhRead       = pdh.NewProc("PdhCollectQueryData")
	pdhArray      = pdh.NewProc("PdhGetFormattedCounterArrayW")
)

func listenerPID(addr *net.TCPAddr) uint32 {
	if addr == nil || !addr.IP.IsLoopback() {
		return 0
	}
	ip := addr.IP.To4()
	family, width, ipOffset, portOffset, pidOffset := uintptr(windows.AF_INET), 24, 4, 8, 20
	if ip == nil {
		ip = addr.IP.To16()
		family, width, ipOffset, portOffset, pidOffset = windows.AF_INET6, 56, 0, 20, 52
	}
	var size uint32
	const listeners = 3 // TCP_TABLE_OWNER_PID_LISTENER
	ret, _, _ := tcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, family, listeners, 0)
	if ret != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size < 4 || size > 8<<20 {
		return 0
	}
	buf := make([]byte, size)
	ret, _, _ = tcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, family, listeners, 0)
	if ret != 0 || size < 4 || int(size) > len(buf) {
		return 0
	}
	var wildcardPID uint32
	rows := int(binary.LittleEndian.Uint32(buf[:4]))
	for offset, i := 4, 0; i < rows && offset+width <= int(size); offset, i = offset+width, i+1 {
		row := buf[offset : offset+width]
		if int(binary.BigEndian.Uint16(row[portOffset:])) != addr.Port {
			continue
		}
		candidate := row[ipOffset : ipOffset+len(ip)]
		pid := binary.LittleEndian.Uint32(row[pidOffset:])
		if bytes.Equal(candidate, ip) {
			return pid
		}
		if net.IP(candidate).IsUnspecified() {
			wildcardPID = pid
		}
	}
	return wildcardPID
}

type processCounters struct {
	Size, FaultCount           uint32
	PeakWorkingSet, WorkingSet uintptr
	MemoryTotals               [6]uintptr
}

func workingSet(process windows.Handle) (uint64, bool) {
	var counters processCounters
	counters.Size = uint32(unsafe.Sizeof(counters))
	r, _, _ := processMemory.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	return uint64(counters.WorkingSet), r != 0 && counters.WorkingSet > 0
}

func openGPUCounter() (query, counter uintptr) {
	if pdhOpen.Find() != nil || pdhAddEnglish.Find() != nil || pdhArray.Find() != nil {
		return 0, 0
	}
	status, _, _ := pdhOpen.Call(0, 0, uintptr(unsafe.Pointer(&query)))
	if status != 0 {
		return 0, 0
	}
	path, _ := windows.UTF16PtrFromString(`\GPU Process Memory(*)\Dedicated Usage`)
	status, _, _ = pdhAddEnglish.Call(query, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&counter)))
	if status != 0 {
		pdhClose.Call(query)
		return 0, 0
	}
	return query, counter
}

type pdhLargeItem struct {
	Name   *uint16
	Status uint32
	_      uint32
	Value  int64
}

func dedicatedGPU(query, counter uintptr, pidPrefix string) (uint64, bool) {
	if query == 0 || unsafe.Sizeof(uintptr(0)) != 8 {
		return 0, false
	}
	status, _, _ := pdhRead.Call(query)
	if status != 0 {
		return 0, false
	}
	const large = 0x00000400 // PDH_FMT_LARGE
	var size, count uint32
	status, _, _ = pdhArray.Call(counter, large, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(status) != 0x800007d2 || size < 24 || size > 4<<20 { // PDH_MORE_DATA
		return 0, false
	}
	buffer := make([]byte, size)
	status, _, _ = pdhArray.Call(counter, large, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buffer[0])))
	if status != 0 || uint64(count)*uint64(unsafe.Sizeof(pdhLargeItem{})) > uint64(len(buffer)) {
		return 0, false
	}
	var total uint64
	seen := false
	for _, item := range unsafe.Slice((*pdhLargeItem)(unsafe.Pointer(&buffer[0])), int(count)) {
		if item.Name != nil && item.Status <= 1 && item.Value >= 0 && strings.HasPrefix(windows.UTF16PtrToString(item.Name), pidPrefix) {
			total += uint64(item.Value)
			seen = true
		}
	}
	return total, seen
}

func observeListenerUsage(parent context.Context, addr *net.TCPAddr) func() *ProcessUsage {
	ctx, stop := context.WithCancel(parent)
	var result *ProcessUsage
	done := make(chan struct{})
	const interval = 500 * time.Millisecond
	go func() {
		defer close(done)
		pid := listenerPID(addr)
		if pid == 0 {
			return
		}
		process, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, pid)
		if err != nil {
			return
		}
		defer windows.CloseHandle(process)
		query, counter := openGPUCounter()
		if query != 0 {
			defer pdhClose.Call(query)
		}
		prefix := "pid_" + strconv.FormatUint(uint64(pid), 10) + "_"
		usage := &ProcessUsage{Source: "windows_loopback_listener_sampled", IntervalMS: int(interval.Milliseconds())}
		sample := func() {
			if value, ok := workingSet(process); ok {
				usage.WorkingSetSamples++
				usage.PeakWorkingSetBytes = max(usage.PeakWorkingSetBytes, value)
			}
			if value, ok := dedicatedGPU(query, counter, prefix); ok {
				usage.DedicatedGPUSamples++
				usage.PeakDedicatedGPUBytes = max(usage.PeakDedicatedGPUBytes, value)
			}
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		sample()
		for {
			select {
			case <-ticker.C:
				sample()
			case <-ctx.Done():
				if usage.WorkingSetSamples > 0 {
					result = usage
				}
				return
			}
		}
	}()
	finish := sync.OnceValue(func() *ProcessUsage {
		stop()
		<-done
		return result
	})
	return finish
}
