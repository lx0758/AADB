package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// recordMaxAge 是记录的保存上限，仅作兜底清理：
// 正常失效由锚点进程存活与否决定，终端开多久都有效
const recordMaxAge = 30 * 24 * 3600

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	// 帮助与版本：不依赖 adb
	switch args[0] {
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return
	case "-V", "--version", "version":
		printVersion(os.Stdout)
		return
	}

	// 前置检查：adb 必须在 PATH 中
	if _, err := exec.LookPath("adb"); err != nil {
		fmt.Fprintln(os.Stderr, "error: adb not found in PATH")
		fmt.Fprintln(os.Stderr, "hint:  add Android SDK platform-tools to PATH")
		os.Exit(1)
	}

	key, anc, err := terminalKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// 子命令
	if args[0] == "forget" {
		old := readTarget(key)
		forgetTarget(key)
		fmt.Fprintf(os.Stderr, "[aadb] forgotten (was: %s)\n", old)
		return
	}
	if args[0] == "list" {
		listTargets()
		return
	}
	if args[0] == "show" {
		t := readTarget(key)
		if t == "" {
			fmt.Fprintln(os.Stderr, "[aadb] no device bound to this terminal")
			return
		}
		fmt.Println(t)
		return
	}

	// 解析 -s：显式指定则覆盖当前终端记录
	target := readTarget(key)
	explicit := false
	if len(args) >= 2 && args[0] == "-s" {
		target = args[1]
		explicit = true
		args = args[2:]
	}

	// 无记录则枚举选择
	if target == "" {
		t, err := pickDevice()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		target = t
		writeTarget(key, target, anc)
		fmt.Fprintf(os.Stderr, "[aadb] %s -> %s\n", key, target)
	}

	// 设备为 ip[:port] 形式时，未连接则先自动 connect，省去手动一步；
	// 省略端口时与 adb 一样默认 5555，并补全后再交给 adb -s
	target = normalizeDeviceAddr(target)
	if err := ensureConnected(target); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// 连接成功后再落盘，避免记住一个连不上的设备
	if explicit {
		writeTarget(key, target, anc)
	}

	// 以子进程方式调用原始 adb
	cmd := exec.Command("adb", append([]string{"-s", target}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// ---------- 会话标识与记录有效性（见 proc_unix.go / proc_windows.go） ----------

// anchor 是一个终端的锚点进程：终端存活期间它保持不变，
// 且启动时刻全局唯一，因此可用于判断记录对应的终端是否还开着
type anchor struct {
	pid int   // 锚点进程 PID（Linux: 会话首进程；Windows: 控制台宿主进程）
	ts  int64 // 锚点启动时刻（平台相关时间基准）
}

// alive 判断锚点进程是否仍在运行。
// 除进程存在外还比对启动时刻，避免 PID 被系统复用后误判为有效
func (a anchor) alive() bool {
	if a.pid <= 0 {
		return false
	}
	cur, err := procStart(a.pid)
	if err != nil {
		return false // 进程已退出，或已无权限访问
	}
	if a.ts == 0 {
		return true // 取不到启动时刻时只能按存在性判断
	}
	return cur == a.ts
}

// record 是一条绑定记录：<key>=<设备>|<锚点PID>|<锚点启动时刻>|<写入时间>
type record struct {
	device    string
	anchor    anchor
	writtenAt int64
}

// state 返回记录状态：active 有效；closed 终端已关闭；expired 超过保存上限
func (r record) state(now int64) string {
	if !r.anchor.alive() {
		return "closed"
	}
	if now-r.writtenAt > recordMaxAge {
		return "expired"
	}
	return "active"
}

func (r record) encode() string {
	return fmt.Sprintf("%s|%d|%d|%d", r.device, r.anchor.pid, r.anchor.ts, r.writtenAt)
}

// parseRecord 解析记录值；格式不符（含旧版两段式）返回 false，按无效记录处理
func parseRecord(v string) (record, bool) {
	parts := strings.Split(v, "|")
	if len(parts) != 4 {
		return record{}, false
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil {
		return record{}, false
	}
	ts, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return record{}, false
	}
	writtenAt, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return record{}, false
	}
	return record{device: parts[0], anchor: anchor{pid: pid, ts: ts}, writtenAt: writtenAt}, true
}

// ---------- 存储：~/aadb.cfg ----------
// 读写分离：loadRecords 只读，prune 负责落盘；
// 所有写操作都以「有效记录」为基础，顺带完成无效记录的清理

// entry 是文件中的一条记录及其 key
type entry struct {
	key string
	rec record
}

func storePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, "aadb.cfg"), nil
}

// loadRecords 读取全部可解析记录，不改动文件
func loadRecords() []entry {
	p, err := storePath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var entries []entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		r, ok := parseRecord(v)
		if !ok {
			continue
		}
		entries = append(entries, entry{key: k, rec: r})
	}
	return entries
}

// activeRecords 返回仍然有效的记录；存在无效记录时顺带从文件中清除
func activeRecords() []entry {
	entries := loadRecords()
	now := time.Now().Unix()
	var active []entry
	for _, e := range entries {
		if e.rec.state(now) == "active" {
			active = append(active, e)
		}
	}
	if len(active) != len(entries) {
		prune(active)
	}
	return active
}

// prune 把给定记录整体写回文件
func prune(entries []entry) {
	p, err := storePath()
	if err != nil {
		return
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, e.key+"="+e.rec.encode())
	}
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// readTarget 返回当前终端绑定的设备；无有效记录时返回空串
func readTarget(key string) string {
	for _, e := range activeRecords() {
		if e.key == key {
			return e.rec.device
		}
	}
	return ""
}

func writeTarget(key, val string, anc anchor) {
	var kept []entry
	for _, e := range activeRecords() { // 顺带清理其他终端的无效记录
		if e.key != key {
			kept = append(kept, e)
		}
	}
	kept = append(kept, entry{key: key, rec: record{device: val, anchor: anc, writtenAt: time.Now().Unix()}})
	prune(kept)
}

func forgetTarget(key string) {
	var kept []entry
	for _, e := range activeRecords() {
		if e.key != key {
			kept = append(kept, e)
		}
	}
	prune(kept)
}

// listTargets 只读展示全部记录及其当前状态，不做清理
func listTargets() {
	entries := loadRecords()
	if len(entries) == 0 {
		fmt.Println("(no records)")
		return
	}
	now := time.Now().Unix()
	for _, e := range entries {
		fmt.Printf("  %s -> %s  [%s, %s]\n", e.key, e.rec.device, ageOf(e.rec.writtenAt, now), e.rec.state(now))
	}
}

// ageOf 把时间差格式化为「多久之前」
func ageOf(t, now int64) string {
	d := now - t
	switch {
	case d < 0:
		return "just now"
	case d < 60:
		return fmt.Sprintf("%ds ago", d)
	case d < 3600:
		return fmt.Sprintf("%dm ago", d/60)
	case d < 86400:
		return fmt.Sprintf("%dh ago", d/3600)
	default:
		return fmt.Sprintf("%dd ago", d/86400)
	}
}

// ---------- 设备枚举 ----------

// listDevices 返回 adb 当前可见的设备及其状态（serial -> state）
func listDevices() map[string]string {
	devs := make(map[string]string)
	out, err := exec.Command("adb", "devices").Output()
	if err != nil {
		return devs
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 {
			devs[f[0]] = f[1]
		}
	}
	return devs
}

// isDeviceAddr 判断设备标识是否为 ip:port（或主机名:端口）形式
func isDeviceAddr(target string) bool {
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" {
		return false
	}
	p, err := strconv.Atoi(port)
	return err == nil && p > 0 && p <= 65535
}

// defaultADBTCPPort 与 adb connect HOST[:PORT] 一致：省略端口时用 5555
const defaultADBTCPPort = "5555"

// normalizeDeviceAddr 把网络设备归一化为可传给 adb -s 的 host:port。
// 与 adb connect 一致，裸 IP 视为省略端口；其他标识（emulator-5554、
// 序列号等）原样返回，避免把普通设备误判成网络设备
func normalizeDeviceAddr(target string) string {
	if isDeviceAddr(target) {
		return target
	}
	if net.ParseIP(target) != nil {
		return net.JoinHostPort(target, defaultADBTCPPort)
	}
	return target
}

// ensureConnected 对 ip:port 形式的设备补上 adb connect：
// 已在线（非 offline）时直接跳过，否则先连接再继续执行原命令
func ensureConnected(target string) error {
	if !isDeviceAddr(target) {
		return nil
	}
	if state, ok := listDevices()[target]; ok && state != "offline" {
		return nil
	}

	fmt.Fprintf(os.Stderr, "[aadb] connecting %s ...\n", target)
	out, err := exec.Command("adb", "connect", target).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg != "" {
			return fmt.Errorf("adb connect %s: %s", target, msg)
		}
		return fmt.Errorf("adb connect %s: %w", target, err)
	}
	// 部分 adb 版本连接失败仍返回 0，以输出判断结果
	if !strings.Contains(msg, "connected to") {
		if msg == "" {
			msg = "unknown error"
		}
		return fmt.Errorf("adb connect %s: %s", target, msg)
	}
	fmt.Fprintf(os.Stderr, "[aadb] %s\n", msg)
	return nil
}

func pickDevice() (string, error) {
	var devs []string
	for serial, state := range listDevices() {
		if state == "device" {
			devs = append(devs, serial)
		}
	}
	if len(devs) == 0 {
		return "", fmt.Errorf("no device found")
	}
	if len(devs) == 1 {
		return devs[0], nil
	}
	sort.Strings(devs) // map 遍历无序，排序保证每次展示一致
	fmt.Println("multiple devices:")
	for i, d := range devs {
		fmt.Printf("  [%d] %s\n", i+1, d)
	}
	fmt.Print("select: ")
	s, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	idx := 0
	fmt.Sscanf(strings.TrimSpace(s), "%d", &idx)
	if idx < 1 || idx > len(devs) {
		return "", fmt.Errorf("invalid choice")
	}
	return devs[idx-1], nil
}
