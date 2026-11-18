# aadb

**auto adb** —— 自动的 adb。

给每个终端绑定一台设备：在同一个终端里，`aadb` 自动复用之前选过的设备，
不用每次敲 `-s <设备>`；开多个终端则各绑各的，互不干扰。

```
PS C:\> aadb shell getprop ro.build.version.release
multiple devices:
  [1] emulator-5554
  [2] 1234ABCD
select: 1
[aadb] win-50354-1338... -> emulator-5554
15

PS C:\> aadb shell echo 第二次不用再选了        # 同一终端直接复用
第二次不用再选了
```

## 特性

- **按终端隔离**：同一终端内自动复用，不同终端窗口/标签页互不影响
- **网络设备自动连接**：`-s ip[:port]` 尚未连接时自动执行 `adb connect`（省略端口默认 5555），省去手动一步
- **终端关闭即失效**：绑定跟着终端走，终端关了下次重新选（不是按时间过期）
- **记录存在 `~/aadb.cfg`**，超过 30 天未更新自动清理
- **零依赖**：单个静态二进制，只要求 `adb` 在 `PATH` 中

## 安装

```bash
git clone <repo> && cd aadb
make build          # 产物在 bin/aadb-<版本> 和 bin/aadb-<版本>.exe
make release        # 交叉编译 linux/windows，产物在 dist/
```

也可以直接从 GitHub Releases 下载对应平台的二进制，放到 `PATH` 里即可。

## 用法

```bash
aadb <adb 参数...>          # 在当前终端绑定的设备上执行 adb，首次会提示选择
aadb -s <设备> <adb 参数>    # 显式指定设备，并覆盖当前终端的绑定
aadb show                   # 回显当前终端绑定的设备
aadb list                   # 查看各终端分别绑定了哪台设备
aadb forget                 # 解除当前终端的绑定（换设备前先执行）
aadb help                   # 显示帮助
aadb -V                     # 显示版本号
```

示例：

```bash
aadb shell getprop ro.build.version.release   # 首次运行选设备并记住
aadb -s emulator-5554 install app.apk         # 指定设备，本终端后续自动复用
aadb -s 192.168.1.20 shell                     # 未连接时会先自动 adb connect
aadb -s 192.168.1.20:5556 shell               # 显式指定端口
aadb logcat -s MyTag                           # 不用再写 -s
aadb show                                      # 回显当前终端绑定的是哪台设备
aadb list
aadb forget
```

## 支持平台

| 平台 | 产物 |
|---|---|
| Windows amd64 | `aadb-<版本>-windows-amd64.exe` |
| Linux amd64 | `aadb-<版本>-linux-amd64` |

macOS 暂不支持（依赖 `/proc`）。

## 版本号

```bash
make build VERSION=1.2.3     # 手动指定
make build                   # 默认取 git describe，无 tag 时为短 hash / dev
```

版本号同时注入到 `aadb -V` 输出和 Windows 文件属性（产品名、公司名、版本号），
推送 `v*` tag（如 `v2026.11.1`）会触发 GitHub Actions 自动构建并发布 Release。
