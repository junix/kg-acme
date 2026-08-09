# 04 — Roadmap

## Phase 0 — 既有 CLI 适配（provider 侧，并行进行）

把存量 CLI 适配到 `kg.provider/v1`：

- `kg-extract`（Rust）：实现 `describe --json` / `available --json` /
  `invoke <cap> --request -`；describe 的 cli_spec 必须与现有 argv 等价
  （等价性测试锁死）。
- `kg-mm`（kg-multimodal）：同上，`parse.multimodal` 能力。
- `ygr`（kg-graphrag）：同上，`retrieve.ask` 能力。

适配完成后 hub 自动从 fallback 桥切换到协议模式（probed 优先），
fallback 表退化为纯兜底。若 provider 自描述与 hub 表漂移，hub 会发射
`cli_spec differs ...` diagnostic 提醒收敛。

## Phase 1 — capability hub 骨架（本仓库当前状态）

- catalog / discovery / router / policy / bridge / schema 全套；
- `kg list` / `kg describe` / `kg <capability>` / `kg provider`；
- 策略门 + dry-run；版本协商；JSON Schema 校验；
- 假 provider 端到端测试。

## Phase 2 — pipeline runner（DAG）

`kg pipeline` 从 stub 变为真正的流水线执行器：

- 声明式流水线文件（YAML/JSON）：stage = `(capability_id, input mapping)`，
  stage 间以 `output.kind` 衔接（kg-document → communities → store）；
- hub 把 stage 编译成 DAG，拓扑序执行，每 stage 复用 Phase 1 的
  resolve / 策略门 / envelope 机制；
- 流水线级策略：各 stage 副作用并集一次过门，一次 dry-run 渲染全图；
- 接口预留（Phase 1 已就位）：
  - `router.Resolve` / `router.Execute` 以 `capability_id` 为单位，天然
    是 pipeline 的单步；
  - envelope 的 `output.kind` / `artifacts[].kind` 是 stage 间类型匹配的
    依据；
  - `catalog.Command{Builtin, Stub}` 已占住 `pipeline` 命令位。

## Phase 3 — kg-mcp 同源展开

同一套 catalog / discovery / router / policy 以 MCP server 形态展开
（`kg mcp` 或独立 `kg-mcp`）：

- 每个 catalog 能力命令 ↔ 一个 MCP tool；tool 的 inputSchema 直接来自
  provider 的 `input_schema`（铁律 2 的天然延伸：LLM 看到的参数面与
  CLI 完全一致）；
- 策略门在 MCP 形态下由 server 配置而非 `--allow-*` flag 提供；
- envelope 即 tool 的结构化输出；diagnostics 映射为 MCP annotations。

## 非目标（长期）

- hub 永远不实现抽取/去重/社区/问答/存储算法本身；
- hub 永远不维护 provider 参数的第二份"权威"描述——fallback 表会随
  provider 协议适配完成而收缩，而不是扩张。
