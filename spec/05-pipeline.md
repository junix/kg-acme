# 05 — Pipeline Runner（Phase 2）

`kg pipeline` 把"解析 → 抽取 → 消歧 → 入库"这类多步流程串成一条命令。
hub 仍然只做编排：每个 stage 是一次 Phase 1 的路由调用
（`router.Resolve` + `router.Execute`），享受 probed 优先、cli_spec 覆盖、
策略门的全部既有语义。stage 之间唯一的通道是 **artifact**。

## 1. 流水线定义：`kg.pipeline/v1`

```json
{
  "pipeline": "kg.pipeline/v1",
  "name": "doc-to-graph",
  "stages": [
    {"id": "parse",   "capability": "parse.multimodal", "optional": true,
     "input": {"sidecar": "sidecar.json"}},
    {"id": "extract", "capability": "extract.entities_relations",
     "input": {"file": "doc.md", "engine": "simple", "backend": "mock"}},
    {"id": "dedup",   "capability": "resolve.coref",
     "input_from": {"stage": "extract", "artifact_kind": "kg-document", "as": "document_file"}},
    {"id": "store",   "capability": "store.triples",
     "input_from": {"stage": "dedup", "artifact_kind": "kg-document", "as": "document_file"}}
  ]
}
```

stage 字段：

| 字段 | 必填 | 含义 |
|---|---|---|
| `id` | 是 | stage 标识，`[a-z0-9-_]+`，定义内唯一 |
| `capability` | 是 | provider 发布命名空间里的 capability_id |
| `optional` | 否 | 失败时跳过（记 diagnostic）而不是中止流水线 |
| `input` | 否 | 静态 input，与 provider cli_spec 的同名属性 |
| `input_from` | 否 | 上游 artifact 注入边，单对象或数组（fan-in 用数组） |

`input_from` 边字段：

- `stage`：上游 stage id；
- `artifact_kind`：按 kind 匹配上游 artifacts；省略时取上游第一个
  artifact。给出时必须等于上游 capability 声明的 `output.kind`；
- `as`：artifact 路径注入到下游 input_schema 的哪个属性
  （如 `document_file`）。

## 2. 类型边（typed edges）

定义期（`pipeline validate` / `pipeline run` 的计划阶段）对每条边做三层
校验，任何一层失败即拒绝，**不进入执行**：

1. **可接线**：上游 capability 必须是 `output.mode == "artifact"`——
   result-json 能力没有 artifact 可传（如 `detect.communities` 不能直接
   喂下游，拒绝）；
2. **kind 一致**：边的 `artifact_kind`（如给出）必须等于上游声明的
   `output.kind`；
3. **通道兼容**：artifact kind 映射到数据通道
   （`kg-document → graph`、`chunks → chunks`、`communities → communities`、
   `json → json`），下游目标属性按 hub 命名约定映射到期望通道
   （`document` / `document_file` → graph，`chunks_file` → chunks；
   表外属性无类型、不拦截）。两侧通道都已知且不同 → 拒绝。

违反任一规则报 **`incompatible_stage_edge`**。`as` 目标属性还必须在
下游 input_schema 中存在（closed schema 下可判定），否则 `invalid_input`。

校验完边之后，stage 的静态 input 与注入占位符
（`kg-pipeline://<stage>/<kind>`）合并，按下游 input_schema 校验——
缺必填属性等错误在计划期就暴露，而不是执行到一半才炸。

## 3. 执行

- **DAG 拓扑序**：stage 按 `input_from` 建图，Kahn 拓扑排序（平手按
  定义序，确定性）。线性链只是退化 DAG，分支/fan-in 无需改代码；
  成环报 `invalid_pipeline`。
- **每 stage 走 router**：`router.Resolve`（probed 优先、weight、
  `--provider` 过滤）+ `router.Execute`（input_schema 校验、策略门、
  协议 invoke / fallback argv）。
- **策略门预检**：计划期收集全部 stage 的 `side_effects` 并集，执行前
  一次过门；缺 `--allow-*` 时 **fail fast**，错误列出所需 flag，任何
  provider 都不会启动。
- **artifact 落 work-dir**：每 stage 成功后，hub 校验 envelope 里
  artifact 的 checksum，把文件复制进 `--work-dir`
  （`stage-<id>-<basename>`），重算 sha256，下游注入的是 work-dir 里的
  副本——provider 临时目录会消失，work-dir 是自包含的。
  默认 work-dir 为 `kg-pipeline-<yyyymmdd-hhmmss>/`（cwd 下）。

## 4. `--dry-run`

零执行渲染完整计划：拓扑序、每 stage 的 provider / capability / 解析后
input（注入位是 `kg-pipeline://...` 占位符）、side_effects；策略门未开
的副作用以 warning diagnostic 列出（含所需 flag）。不创建 work-dir，
不启动 provider，永远 exit 0（计划本身非法除外）。

## 5. 断点重跑：`--resume <work-dir>`

每 stage 完成（成功、失败、跳过）即落
`<work-dir>/stage-<id>.envelope.json`（内容即 envelope 里该 stage 的
StageResult）；整个流水线结束落 `pipeline.envelope.json`。

`--resume <dir>` 时，对每个 stage：记录存在且 `status == "ok"`，且其
全部 artifact 仍存在、sha256 与记录一致 → **跳过执行**，复用 artifact
（记 info diagnostic `reused from ...`）。checksum 不符（文件被改/删）
则该 stage 正常重跑，下游随之用新 artifact。resume 按 stage id 匹配，
定义文件变更（capability/input 改了但 id 没变）不会使缓存失效——
这是有意的简单语义，变了定义就换 work-dir。

## 6. 失败语义

- stage 失败（provider envelope error、checksum 不符、上游 artifact
  不可得）：默认**中止**，流水线 envelope `status: "error"`，`stages`
  含已完成 stage 的结果与失败 stage 的 `error`；
- `optional: true` 的 stage 失败：记 warning diagnostic，status 置
  `skipped`，继续。下游若 `input_from` 一个被跳过的 stage，会因
  "上游 artifact 不可得"失败（除非它自己也 optional）。

## 7. 命令面与输出契约

```
kg pipeline run <def.json> [--dry-run] [--work-dir d | --resume d] [--json] [--allow-*] [--provider id] [--provider-bin ID=PATH]
kg pipeline validate <def.json> [--json]
```

`validate` = 完整计划（结构、capability 解析、类型边、静态 input
schema），零执行。

`--json` 时 stdout **恰好一个** `kg.pipeline.execution/v1` envelope：

```json
{
  "protocol": "kg.pipeline.execution/v1",
  "pipeline": "doc-to-graph",
  "status": "ok | error",
  "work_dir": "kg-pipeline-20260809-120000",
  "dry_run": false,
  "stages": [
    {"id": "extract", "capability": "extract.entities_relations",
     "provider": "kg-extract", "status": "ok | error | skipped | planned",
     "artifacts": [{"path": "...", "kind": "kg-document", "checksum": "sha256:..."}],
     "error": {"code": "...", "message": "..."}}
  ],
  "diagnostics": [{"severity": "...", "message": "..."}],
  "error": {"code": "...", "message": "..."}
}
```

新增错误码：`invalid_pipeline`（结构/环/未知 stage 引用）、
`incompatible_stage_edge`（类型边不兼容）；其余复用
`capability_not_found` / `invalid_input` / `policy_denied` /
`invocation_failed`。

## 8. 与 Phase 3（kg-mcp）的接口预留

- 流水线定义是纯数据（kg.pipeline/v1 JSON），MCP 形态下可直接作为
  tool 的输入文档；
- `pipeline.Build` / `pipeline.Execute` 以 Go API 暴露，不依赖 CLI
  flag 解析——MCP server 可用 server 配置的 Gates 调用同一入口；
- pipeline envelope 与 stage StageResult 是结构化输出，可原样映射为
  MCP tool 的 structured content。
