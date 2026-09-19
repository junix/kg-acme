# CLAUDE.md — kg-acme

## 这是什么

KG 工具链的 capability hub（Go）。acme 家族模式：hub 统一发现/路由/治理
provider，provider 提供真实能力。入口 `cmd/kg`，逻辑全在 `internal/`。

## acme 铁律（修改任何代码前先读）

1. **hub 只做集成**：发现 / 协议 / catalog / 策略 / 路由。
   **绝不**在 hub 里实现 KG 算法（抽取、去重、社区、问答、存储……）。
   发现自己在 hub 里写算法 = 停下来，那属于 provider。
2. **hub 不写死 provider 的选项/枚举**。参数面永远来自 provider 的
   `describe --json`（cli_spec + input_schema）。`internal/bridge` 的
   fallback 表是兜底数据，不是权威；provider 自描述冲突时以 provider
   为准并发射 diagnostic。不要在 fallback 表之外任何地方硬编码
   provider 的 flag、枚举、默认值。

## 构建/测试

- `just build` / `just test` / `just vet` / `just check`
- 等价裸命令：`go build ./...`、`go test ./...`、`go vet ./...`
- 测试分两层：`internal/*` 单测 + `tests/` 端到端（go build 真二进制 +
  shell 假 provider 脚本；PATH 须保留系统路径供脚本用 coreutils）。

## 约定

- catalog 是唯一稳定命令面：`internal/catalog/catalog.json`，改它必须过
  `Load()` 校验（semantic_id 镜像 command_path、title 无结尾标点、
  description 单句以 `.` 结尾）。
- 协议类型在 `internal/protocol`；错误码集合固定在那里，新增错误先想
  清楚是否已有合适码（尤其 `malformed_manifest` vs
  `unsupported_schema_version` 不可混用）。
- `--json` 时 stdout **恰好一个** envelope；日志/诊断永远 stderr。
- 副作用门默认全拒，新增副作用类型要同步 `policy.AllowFlag` 映射与
  `spec/03-policy-gates.md`。
- 文档与代码同步：改协议 → 改 `spec/01`；改命令面 → 改 `spec/02` 与
  README；改策略 → 改 `spec/03`。

## 环境

- macOS，Go（homebrew，`go` 在 PATH）。
- provider 发现涉及 `~/sync/<os>-<arch>-bin/` 与 `~/sync/bin/`。
- 本仓库位于 `~/projects/kg/`（算法/逻辑侧）；存储适配器属于
  `~/projects/graphdb/`，不要越界。
