# aadb 构建脚本
#
# 常用命令：
#   make build          交叉编译 linux + windows 到 bin/
#   make dev            直接运行
#   make test           运行全部测试
#   make winres         生成 Windows 版本资源（.syso）
#   make clean          清理产物
#
# 版本号默认取 git describe，可用 VERSION 覆盖：
#   make build VERSION=1.2.3

.PHONY: init dev test build build-linux build-windows winres winres-clean clean

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")

# Windows 版本资源（PE 的 .rsrc 段）。
#
# 由 build/winres/versioninfo.json 生成 resource_windows_<arch>.syso，
# 放在主包目录下。文件名带 _windows_<arch> 后缀，go 命令只会把它们
# 链进对应平台的构建，因此 Linux 产物不受影响。
#
# 带上产品名、公司名与版本号后，文件属性不再是「来源不明的程序」，
# 可降低被杀毒软件误判的概率。
WINRES_DIR      := build/winres
WINRES_TEMPLATE := $(WINRES_DIR)/versioninfo.json

init:
	go mod tidy

dev:
	go run .

test:
	go test ./...

build: build-linux build-windows

build-linux:
	echo "Building aadb for linux/amd64"
	GOOS=linux GOARCH=amd64 CC=gcc go build -ldflags '-X main.Version=$(VERSION)' -o bin/aadb-$(VERSION) -v .
	echo "Building aadb for linux/amd64 done"

build-windows: winres
	echo "Building aadb for windows/amd64"
	GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -ldflags '-X main.Version=$(VERSION)' -o bin/aadb-$(VERSION).exe -v .
	echo "Building aadb for windows/amd64 done"

# 生成 Windows 版本资源。
#
# 版本号从 VERSION 解析：形如 v1.2.3 的语义化版本拆成主/次/修订号；
# 无法解析时（如 dev 或纯 commit hash）统一用 0.0.0。
winres:
	@v=$$(echo "$(VERSION)" | sed 's/^v//'); \
	num() { case "$$1" in ''|*[!0-9]*) echo 0 ;; *) echo "$$1" ;; esac; }; \
	major=$$(num "$$(echo "$$v" | cut -d. -f1 | sed 's/[^0-9].*//')"); \
	minor=$$(num "$$(echo "$$v" | cut -d. -f2 | sed 's/[^0-9].*//')"); \
	patch=$$(num "$$(echo "$$v" | cut -d. -f3 | sed 's/[^0-9].*//')"); \
	fv="$$major.$$minor.$$patch.0"; \
	echo "生成 Windows 版本资源（版本 $$fv）"; \
	go tool goversioninfo -platform-specific \
		-ver-major $$major -ver-minor $$minor -ver-patch $$patch -ver-build 0 \
		-product-ver-major $$major -product-ver-minor $$minor \
		-product-ver-patch $$patch -product-ver-build 0 \
		-file-version "$$fv" -product-version "$$fv" \
		"$(CURDIR)/$(WINRES_TEMPLATE)" || exit 1; \
	echo "已生成："; ls resource_windows_*.syso

winres-clean:
	@rm -f resource_windows_*.syso

clean: winres-clean
	rm -rf bin

.DEFAULT_GOAL := build
