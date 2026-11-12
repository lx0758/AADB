package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

// Version 版本号，构建时通过 -ldflags '-X main.Version=x.y.z' 注入
var Version = "dev"

// printVersion 输出单行版本信息
func printVersion(w io.Writer) {
	fmt.Fprintf(w, "aadb %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
}

// printUsage 输出帮助信息：分节展示，输出到终端时着色
func printUsage(w io.Writer) {
	bold := painter(w, "1")    // 标题加粗
	dim := painter(w, "2")     // 补充说明弱化
	accent := painter(w, "36") // 命令与版本青色

	fmt.Fprintf(w, "%s %s  %s\n", bold("aadb"), accent(Version), "auto adb · 自动的 adb：按终端自动绑定设备")
	fmt.Fprintln(w)

	printSection(w, bold, accent, "用法", [][2]string{
		{"aadb [-s <设备>] <adb 参数...>", "在当前终端绑定的设备上执行 adb 命令"},
		{"aadb list", "查看所有终端的绑定记录"},
		{"aadb forget", "清除当前终端的绑定"},
		{"aadb version | -V | --version", "显示版本号"},
		{"aadb help | -h | --help", "显示本帮助"},
	})

	printSection(w, bold, accent, "选项", [][2]string{
		{"-s <设备>", "显式指定设备，并覆盖当前终端已有的绑定"},
		{"-h, --help", "显示本帮助"},
		{"-V, --version", "显示版本号"},
	})

	printSection(w, bold, accent, "示例", [][2]string{
		{"aadb shell getprop ro.build.version.release", "首次运行会列出设备供选择并记住"},
		{"aadb -s emulator-5554 install app.apk", "指定设备，本终端后续命令自动复用"},
		{"aadb list", "查看各终端分别绑定了哪台设备"},
		{"aadb forget", "想换设备时先解除当前绑定"},
	})

	fmt.Fprintf(w, "%s\n", bold("行为说明"))
	for _, note := range []string{
		"绑定按终端隔离：同一终端内自动复用，不同终端互不影响",
		"记录保存在 ~/aadb.cfg，终端关闭即失效；超过 30 天未更新一并清理",
		"运行前需保证 adb 已加入 PATH",
	} {
		fmt.Fprintf(w, "  %s\n", dim("· "+note))
	}
}

// printSection 打印一个「命令 + 说明」的分节，两列按显示宽度对齐
func printSection(w io.Writer, bold, accent func(string) string, title string, rows [][2]string) {
	fmt.Fprintf(w, "%s\n", bold(title))

	width := 0
	for _, r := range rows {
		if n := displayWidth(r[0]); n > width {
			width = n
		}
	}
	for _, r := range rows {
		fmt.Fprintf(w, "  %s  %s\n", accent(padRight(r[0], width)), r[1])
	}
	fmt.Fprintln(w)
}

// painter 返回着色函数：w 是终端时输出 ANSI 序列，否则原样返回
func painter(w io.Writer, attrs string) func(string) string {
	color := false
	if f, ok := w.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			color = fi.Mode()&os.ModeCharDevice != 0
		}
	}
	if !color {
		return func(s string) string { return s }
	}
	return func(s string) string {
		return "\x1b[" + attrs + "m" + s + "\x1b[0m"
	}
}

// padRight 按终端显示宽度向右补齐空格
func padRight(s string, width int) string {
	for displayWidth(s) < width {
		s += " "
	}
	return s
}

// displayWidth 估算字符串的终端显示宽度，CJK 与全角字符按 2 列计
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		if isWideRune(r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func isWideRune(r rune) bool {
	return (r >= 0x1100 && r <= 0x115f) || // 韩文字母等
		(r >= 0x2e80 && r <= 0xa4cf) || // CJK 部首 ~ 假名
		(r >= 0xac00 && r <= 0xd7a3) || // 谚文音节
		(r >= 0xf900 && r <= 0xfaff) || // CJK 兼容表意文字
		(r >= 0xfe30 && r <= 0xfe6f) || // CJK 兼容形式
		(r >= 0xff00 && r <= 0xff60) || // 全角形式
		(r >= 0xffe0 && r <= 0xffe6) // 全角符号
}
