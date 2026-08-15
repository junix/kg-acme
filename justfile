# kg-acme — capability hub for the KG toolchain

set shell := ["bash", "-euo", "pipefail", "-c"]

# 编译型二进制安装目录（区分系统与架构，ADR-749）
os_suffix := if os() == "macos" { "macos" } else { "linux" }
arch_suffix := if arch() == "aarch64" { "arm64" } else { "x86" }
install_bin := env("SYNC_BIN_DIR", home_directory() / "sync" / (os_suffix + "-" + arch_suffix + "-bin"))

default: test

build:
    go build ./...
    go build -o kg ./cmd/kg
    go build -o kgctl ./cmd/kgctl
    go build -o kg-mcp ./cmd/kg-mcp

test:
    go test ./...

vet:
    go vet ./...

check: vet test build

# 安装执行面、控制面与 MCP，并发布不可变能力快照。
install: build
    mkdir -p "{{ install_bin }}"
    cp kg "{{ install_bin }}/kg"
    cp kgctl "{{ install_bin }}/kgctl"
    cp kg-mcp "{{ install_bin }}/kg-mcp"
    "{{ install_bin }}/kgctl" refresh

clean:
    rm -f kg kgctl kg-mcp
