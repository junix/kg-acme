# kg-acme — capability hub for the KG toolchain

set shell := ["bash", "-euo", "pipefail", "-c"]

# 编译型二进制安装目录（区分系统与架构，ADR-749）
os_suffix := if os() == "macos" { "macos" } else { "linux" }
arch_suffix := if arch() == "aarch64" { "arm64" } else { "x86" }
install_bin := env("SYNC_BIN_DIR", home_directory() / "sync" / (os_suffix + "-" + arch_suffix + "-bin"))

# 构建印章（ADR-1168）：git 短 sha，工作树脏时带 .dirty 后缀
stamp := `git rev-parse --short HEAD` + ` (git diff --quiet && git diff --cached --quiet) >/dev/null 2>&1 || printf .dirty`
ldflags := "-X kg-acme/internal/cli.Version=0.2.0+g" + stamp

default: test

build:
    go build ./...
    go build -ldflags "{{ ldflags }}" -o kg ./cmd/kg
    go build -ldflags "{{ ldflags }}" -o kgctl ./cmd/kgctl
    go build -ldflags "{{ ldflags }}" -o kg-mcp ./cmd/kg-mcp

test:
    go test ./...

vet:
    go vet ./...

check: vet test build

# 安装执行面、控制面与 MCP，并发布不可变能力快照。
install: build
    mkdir -p "{{ install_bin }}"
    @set -eu; dest="{{ install_bin }}/kg"; mkdir -p "$(dirname "$dest")"; tmp="$(mktemp "{{ install_bin }}/.kg.XXXXXX")"; trap 'rm -f "$tmp"' EXIT; cp "kg" "$tmp"; chmod 755 "$tmp"; if [ "$(uname -s)" = "Darwin" ]; then xattr -c "$tmp" 2>/dev/null || true; codesign --force --sign - "$tmp"; fi; mv -f "$tmp" "$dest"
    @set -eu; dest="{{ install_bin }}/kgctl"; mkdir -p "$(dirname "$dest")"; tmp="$(mktemp "{{ install_bin }}/.kgctl.XXXXXX")"; trap 'rm -f "$tmp"' EXIT; cp "kgctl" "$tmp"; chmod 755 "$tmp"; if [ "$(uname -s)" = "Darwin" ]; then xattr -c "$tmp" 2>/dev/null || true; codesign --force --sign - "$tmp"; fi; mv -f "$tmp" "$dest"
    @set -eu; dest="{{ install_bin }}/kg-mcp"; mkdir -p "$(dirname "$dest")"; tmp="$(mktemp "{{ install_bin }}/.kg-mcp.XXXXXX")"; trap 'rm -f "$tmp"' EXIT; cp "kg-mcp" "$tmp"; chmod 755 "$tmp"; if [ "$(uname -s)" = "Darwin" ]; then xattr -c "$tmp" 2>/dev/null || true; codesign --force --sign - "$tmp"; fi; mv -f "$tmp" "$dest"
    "{{ install_bin }}/kgctl" refresh

clean:
    rm -f kg kgctl kg-mcp
