//go:build !windows

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// clockTicks 是 /proc 时间片频率，Linux 用户态恒为 100 (USER_HZ)
const clockTicks = 100

// terminalKey 以「控制终端 + 会话首进程」标识当前终端：
// 同一终端内（含管道、make、子 shell）共享，不同终端窗口/标签页相互隔离。
// 返回的 anchor 是该终端的锚点进程，用于判断记录对应的终端是否还开着。
func terminalKey() (string, anchor, error) {
	sid, ttyNr, _, err := procStat(os.Getpid())
	if err != nil {
		return "", anchor{}, err
	}
	if ttyNr != 0 {
		// 会话首进程（sid 对应的进程）在终端存活期间保持不变，
		// 其启动时刻全局唯一，可同时充当 key 的稳定成分与校验锚点
		if ts, err := procStart(int(sid)); err == nil {
			return fmt.Sprintf("tty-%d-%d-%d", ttyNr, sid, ts), anchor{pid: int(sid), ts: ts}, nil
		}
	}
	// 没有控制终端（cron、守护进程），或会话首进程已不可读：退回进程树根
	a := rootAnchor()
	return fmt.Sprintf("pid-%d-%d", a.pid, a.ts), a, nil
}

// rootAnchor 沿父链上溯到进程树根，以其作为锚点（适用于没有控制终端的场景）
func rootAnchor() anchor {
	pid := os.Getpid()
	for {
		ppid, err := parentPID(pid)
		if err != nil || ppid == 0 || ppid == pid {
			break
		}
		pid = ppid
	}
	ts, err := procStart(pid)
	if err != nil {
		return anchor{pid: pid} // 读不到启动时刻，只能按存在性校验
	}
	return anchor{pid: pid, ts: ts}
}

// procStat 解析 /proc/<pid>/stat，返回会话 ID(session，第 6 字段)、
// 控制终端设备号(tty_nr，第 7 字段)与启动时钟节拍(starttime，第 22 字段)
func procStat(pid int) (sid, ttyNr, start int64, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, 0, err
	}
	// 第 2 字段 comm 可能含空格与右括号，需从最后一个 ')' 之后切分
	s := string(data)
	idx := strings.LastIndexByte(s, ')')
	if idx < 0 {
		return 0, 0, 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	f := strings.Fields(s[idx+1:])
	// f[0] 是第 3 字段 state，故 session=下标3、tty_nr=下标4、starttime=下标19
	if len(f) < 20 {
		return 0, 0, 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	if sid, err = strconv.ParseInt(f[3], 10, 64); err != nil {
		return 0, 0, 0, fmt.Errorf("malformed session in /proc/%d/stat: %w", pid, err)
	}
	if ttyNr, err = strconv.ParseInt(f[4], 10, 64); err != nil {
		return 0, 0, 0, fmt.Errorf("malformed tty_nr in /proc/%d/stat: %w", pid, err)
	}
	if start, err = strconv.ParseInt(f[19], 10, 64); err != nil {
		return 0, 0, 0, fmt.Errorf("malformed starttime in /proc/%d/stat: %w", pid, err)
	}
	return sid, ttyNr, start, nil
}

// procStart 返回进程启动时刻（epoch 秒）
func procStart(pid int) (int64, error) {
	_, _, start, err := procStat(pid)
	if err != nil {
		return 0, err
	}
	boot, err := bootTime()
	if err != nil {
		return start / clockTicks, nil // 拿不到开机时间时退回本次启动内的相对值
	}
	return boot + start/clockTicks, nil
}

// bootTime 返回系统启动时刻（epoch 秒），取自 /proc/stat 的 btime 行
func bootTime() (int64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "btime ") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, "btime "))
		btime, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("malformed btime %q", v)
		}
		return btime, nil
	}
	return 0, fmt.Errorf("btime not found in /proc/stat")
}

// parentPID 读取 /proc/<pid>/status 的 PPid 字段
func parentPID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "PPid:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, "PPid:"))
		ppid, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("malformed PPid %q", v)
		}
		return ppid, nil
	}
	return 0, fmt.Errorf("PPid not found for pid %d", pid)
}
