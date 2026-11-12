//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// syscall 包未导出该访问权，按 Win32 定义补上
const processQueryLimitedInformation = 0x1000

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	moduser32   = syscall.NewLazyDLL("user32.dll")

	procGetConsoleWindow         = modkernel32.NewProc("GetConsoleWindow")
	procGetWindowThreadProcessId = moduser32.NewProc("GetWindowThreadProcessId")
)

// terminalKey 以「控制台窗口 + 窗口宿主进程」标识当前终端：
// 同一控制台内（含子进程）共享，不同窗口/Windows Terminal 标签页相互隔离。
// 返回的 anchor 是该终端的锚点进程，用于判断记录对应的终端是否还开着。
//
// Windows 的父进程链可能落到已退出或无权限的 PID 上（OpenProcess 分别返回
// ERROR_INVALID_PARAMETER / ERROR_ACCESS_DENIED），因此这条路径一律降级、不报错。
func terminalKey() (string, anchor, error) {
	if hwnd, host := consoleWindow(); hwnd != 0 {
		// 宿主进程的创建时间既稳定，又可抵消 HWND 值被系统复用的情况
		if host != 0 {
			if ts, err := procStart(int(host)); err == nil {
				return fmt.Sprintf("win-%x-%d", hwnd, ts), anchor{pid: int(host), ts: ts}, nil
			}
		}
		// 拿不到宿主进程：窗口仍可区分终端，锚点退回进程树根（由保存上限兜底）
		a := rootAnchor()
		return fmt.Sprintf("win-%x-%d", hwnd, a.ts), a, nil
	}
	// 没有控制台（无窗口进程）：退回进程树根
	a := rootAnchor()
	return fmt.Sprintf("pid-%d-%d", a.pid, a.ts), a, nil
}

// consoleWindow 返回控制台窗口句柄及其宿主进程 PID：
// Windows Terminal 下宿主是各标签页独立的 openconsole.exe，传统控制台下是该窗口的根进程。
// 无控制台（CREATE_NO_WINDOW / 服务）时返回 0, 0
func consoleWindow() (hwnd, host uintptr) {
	hwnd, _, _ = procGetConsoleWindow.Call()
	if hwnd == 0 {
		return 0, 0
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return hwnd, uintptr(pid)
}

// rootAnchor 沿父链上溯到进程树根，以其作为锚点。
// 读不到启动时间时按存在性校验：父链上可能有已退出（ERROR_INVALID_PARAMETER）
// 或无权限（ERROR_ACCESS_DENIED，如 csrss/System）的进程，取不到就用上一个能读到的
func rootAnchor() anchor {
	pid := os.Getpid()
	var ts int64
	for {
		ppid, err := parentPID(pid)
		if err != nil || ppid == 0 || ppid == pid {
			break
		}
		pid = ppid
		if s, err := procStart(pid); err == nil {
			ts = s
		}
	}
	if ts == 0 {
		return anchor{pid: pid}
	}
	return anchor{pid: pid, ts: ts}
}

// procStart 返回进程启动时刻（FILETIME 原始值：100ns 单位，自 1601-01-01 起）
func procStart(pid int) (int64, error) {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(h)

	var create, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &create, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	return int64(create.HighDateTime)<<32 | int64(create.LowDateTime), nil
}

// parentPID 通过 Toolhelp32 进程快照查询父进程 PID
func parentPID(pid int) (int, error) {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(snap)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := syscall.Process32First(snap, &entry); err == nil; err = syscall.Process32Next(snap, &entry) {
		if int(entry.ProcessID) == pid {
			return int(entry.ParentProcessID), nil
		}
	}
	return 0, fmt.Errorf("process %d not found", pid)
}
