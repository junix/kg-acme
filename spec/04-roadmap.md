# 04 — Roadmap

## Phase 0 — 既有 CLI 适配（provider 侧，并行进行）

把存量 CLI 适配到 `kg.provider/v1`：

- `kg-extract`（Rust）：实现 `describe --json` / `available --json` /
  `invoke <cap> --request -`；describe 的 cli_spec 必须与现有 argv 等价
  （等价性测试锁死）。
- `kg-mm`（kg-multimodal）：同上，`parse.multimodal` 能力。
- `ygr`（kg-graphrag）：同上，`retrieve.ask` 能力。

已完成适配的协议原生 provider（无 legacy argv 形态，无 fallback 桥；
hub 经 `router.ProtocolNativeBins` 按名发现）：

- `kg-layout`（kg-layout，Python）：`layout.compute` / `layout.draw`。
- `graph-kg`（graph，Python）：`analyze.centrality` /
  `detect.communities_semantic` / `embed.nodes`。

适配完成后 hub 自动从 fallback 桥切换到协议模式（probed 优先），
fallback 表退化为纯兜底。若 provider 自描述与 hub 表漂移，hub 会发射
`cli_spec differs ...` diagnostic 提醒收敛。

## Phase 1 — capability hub 骨架（本仓库当前状态）

- catalog / discovery / router / policy / bridge / schema 全套；
- `kg list` / `kg describe` / `kg <capability>` / `kg provider`；
- 策略门 + dry-run；版本协商；JSON Schema 校验；
- 假 provider 端到端测试。

## Phase 2 — pipeline runner（DAG，已完成，见 spec/05）

`kg pipeline` 已从 stub 变为真正的流水线执行器：

- 声明式流水线文件（`kg.pipeline/v1` JSON）：stage =
  `(capability_id, input, input_from)`，stage 间以 artifact kind + 通道
  兼容的类型边衔接（kg-document → document_file 等）；
- hub 把 stage 编译成 DAG，Kahn 拓扑序执行，每 stage 复用 Phase 1 的
  resolve / 策略门 / envelope 机制；
- 流水线级策略：各 stage 副作用并集一次过门（fail fast 列出所需
  flag），`--dry-run` 一次渲染全图；
- artifact 统一落 `--work-dir`（含 checksum 校验与复制），
  `--resume <dir>` 按 stage envelope 断点重跑；
- `optional: true` stage 失败跳过记 diagnostic，其余失败中止；
- 命令面：`kg pipeline run <def.json>` / `kg pipeline validate <def.json>`，
  `--json` 输出 `kg.pipeline.execution/v1` envelope。

## Phase 3 — kg-mcp 同源展开（已完成，见 spec/06）

同一套 catalog / discovery / router / policy / pipeline 以 MCP server
形态展开（独立 `cmd/kg-mcp`，stdio JSON-RPC 2.0）：

- 每个 catalog 命令 ↔ 一个 MCP tool（`kg_extract`、`kg_pipeline_run`…）；
  能力 tool 的 inputSchema 直接来自 provider 的 `input_schema`
  （铁律 2 的天然延伸：LLM 看到的参数面与 CLI 完全一致）；
- 策略门由 server 启动配置供给（`--allow-*` flag / `KG_ACME_ALLOW`
  env），未授权返回 policy_denied 结构化错误，不 crash；
- envelope 即 tool 的 structured content；大 artifact 只回
  path+checksum。

## 非目标（长期）

- hub 永远不实现抽取/去重/社区/问答/存储算法本身；
- hub 永远不维护 provider 参数的第二份"权威"描述——fallback 表会随
  provider 协议适配完成而收缩，而不是扩张。
