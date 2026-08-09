# 02 — Catalog 与路由

## Catalog：稳定命令面

`internal/catalog/catalog.json`（go:embed）声明 hub 的**稳定命令**。
这是 hub 对外的长期契约，provider 可以来来去去，命令面不变。

| 命令 | capability_id | 说明 |
|---|---|---|
| `extract` | `extract.entities_relations` | 文档 → 知识图谱 |
| `dedup` | `resolve.coref` | 实体/关系 coref 去重合并 |
| `communities` | `detect.communities` | 社区检测（平铺） |
| `communities hierarchy` | `detect.communities_hierarchy` | 层级社区检测 |
| `communities summaries` | `summarize.communities` | GraphRAG 社区摘要 |
| `store` | `store.triples` | 写入图数据库 |
| `ask` | `retrieve.ask` | GraphRAG 问答 |
| `parse` | `parse.multimodal` | 多模态文档解析 → chunks |
| `provider` | （builtin） | 长尾逃生口：raw 协议调用 |
| `pipeline` | （builtin, stub） | Phase 2 流水线 |

capability id 的唯一真相源是 **provider 的发布命名空间**
（`extract.*` / `detect.*` / `summarize.*` / `resolve.*` / `store.*` /
`retrieve.*` / `parse.*`）；catalog 只是稳定命令到该命名空间的映射。
旧 `kg.*` 命名空间已退役（无已发布用户），hub 不再识别。

两个映射决策（读 provider 发布面后的连贯选择）：

- `--canonical-direction` 不是 `dedup` 的参数：它是 kg-extract 为
  `extract.entities_relations` 发布的 bool flag（抽取时归正方向），
  独立能力 `resolve.canonical_direction` 经 `kg provider` 逃生口触达。
  `dedup` 只映射 `resolve.coref`。
- 社区三能力走**子命令**而非 flag 切换：hub 不在命令里硬编码能力
  语义，`communities hierarchy` / `communities summaries` 是独立的
  catalog entry（semantic_id 镜像两段 command_path）。

每条 entry 的约束（`catalog.Load()` 强制校验，非法即拒绝启动）：

- `semantic_id` 必须**镜像** `command_path`（段以空格连接）；
- 每个 path 段必须是小写 kebab-case（`^[a-z][a-z0-9-]*$`）；
- `title` 非空且**不带结尾标点**；
- `description` 必须是一句以 `.` 结尾的句子；
- capability 命令必须声明 `capability_id`，builtin 命令必须不声明。

catalog **不**声明任何 provider 的 flag/枚举——那是 provider
自描述（或 fallback 桥表）的职责。

## Discovery：provider 发现顺序

`discover.FindExecutable(name, overrides, env)` 按如下顺序解析，只认
**实际可执行**的普通文件：

1. `--provider-bin ID=PATH` 显式覆盖（最高优先，不可执行则穿透到下一级）；
2. `~/sync/<os>-<arch>-bin/`（如 `~/sync/darwin-arm64-bin/`；darwin 上
   同时识别本机约定别名 `~/sync/macos-<arch>-bin/`）；
3. `~/sync/bin/`；
4. `PATH` 查找；
5. `PATH` 扫描 `kg-provider-*` 前缀可执行文件（协议原生 provider）。

发现的每个 provider 做 **best-effort probe**：

- `describe --json` → 校验 provider-v1 schema → 版本协商 →
  填 `ProviderStatus{Manifest, Version, Probed}`；失败分级为
  `malformed_manifest` / `unsupported_schema_version`，记入 diagnostics；
- `available --json` → 填 `ProviderStatus.Available`；失败则留 nil
  （未知），**不降级**（fail-safe）。

## Routing：capability → provider

`router.Resolve(providers, capability_id)`：

1. 候选 = 声明了该 capability 的所有 provider；
2. 排序：**probed 优先于 fallback**，然后 weight 降序，然后 id 字典序；
3. `--provider <id>` 在 CLI 层先过滤候选集。

probed provider 的 `cli_spec` 覆盖 hub fallback 表；不一致时发射
diagnostic（provider 权威，hub 表只是 fallback）。

fallback 表（`internal/bridge`）按 provider 发布命名空间挂键，只桥接
**有真实 argv 调用形态**的能力：`kg-extract`（`extract.entities_relations`）、
`kg-mm`（`parse.multimodal`）、`ygr`（`retrieve.ask`）。graph-in 能力
（`detect.communities`、`resolve.coref`、`store.triples` 等）的输入文档
只能经 `invoke --request -` 的 stdin 传入，没有可渲染的 argv 形态，因而是
protocol-only——未 probe 时对这些能力报 `capability_not_found`，而不是
渲染一个必然失败的 argv。

## 执行路径

- **probed**：`invoke <capability_id> --request -`，stdin 送
  `{"capability_id","input"}`，stdout 收 envelope，hub 透传（合并自身
  diagnostics）。
- **fallback**：`bridge.RenderArgv(cli_spec, input)` 渲染 argv 直接执行；
  `result-json` 模式把 stdout 尝试解析为 JSON 放入 envelope `result`
  （解析失败则包成 `{"stdout": ...}`）；`artifact` 模式按 `stdout:true`
  flag 的值读文件并计算 sha256 写入 `artifacts`。

## 逃生口

`kg provider <id> <capability_id> --request <file|->` 把任意 JSON 请求
直接发给 provider——新 capability 上线不必等 hub 更新 catalog。
