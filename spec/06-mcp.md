# 06 — kg-mcp：同源展开的 MCP server（Phase 3）

`kg-mcp` 把 hub 以 MCP server（stdio）形态展开，供 Claude Code / Claude
Desktop 等 MCP client 驱动。**同一个 hub**：catalog、provider 发现、
router、policy、pipeline 全部复用 `internal/` 的既有实现，MCP 只是
第三种前端（前两种：`kg` 命令行、`kg pipeline`）。

## 1. 传输与协议面

- stdio，**换行分隔 JSON-RPC 2.0** 帧（MCP stdio 标准）；stdout 只载
  协议帧，日志永远 stderr。
- 方法最小集：`initialize`（版本协商）、`ping`、`tools/list`、
  `tools/call`；notifications（`notifications/initialized` 等一切无
  id 帧）一律容忍不回；未知方法回 `-32601`，非法 JSON 回 `-32700`
  且连接不中断。
- 版本协商：认识 `2024-11-05` / `2025-03-26` / `2025-06-18` 则回显，
  否则回 `2025-06-18`。

### SDK 选型：手搓最小集，不用官方 go-sdk

评估过 `github.com/modelcontextprotocol/go-sdk`（v1.7.0）：它会把
`x/oauth2`、`golang-jwt/jwt`、`segmentio/encoding`、`uritemplate`、
`x/tools` 等 8+ 个间接依赖拖进 vendor（HTTP 传输、auth 中间件、
client SDK 全量），并把 go directive 从 1.23 顶到 1.25——为一个
只需要 4 个方法的 stdio server 引入这些是不成比例的。`internal/mcp`
手搓了约 200 行的帧循环 + 方法分发，零新增依赖（模块仍只有
santhosh-tekuri/jsonschema 一个直接依赖）。

## 2. 同源展开（铁律 2 的延伸）

tool 面由 **catalog 运行时派生**，不另维护清单：

| catalog 命令 | tool | inputSchema 来源 |
|---|---|---|
| `extract` | `kg_extract` | probed provider 的 `input_schema`（原样），否则 fallback 表 |
| `dedup` | `kg_dedup` | 同上 |
| `communities` | `kg_communities` | 同上 |
| `communities hierarchy` | `kg_communities_hierarchy` | 同上 |
| `communities summaries` | `kg_communities_summaries` | 同上 |
| `communities semantic` | `kg_communities_semantic` | 同上 |
| `store` | `kg_store` | 同上 |
| `ask` | `kg_ask` | 同上 |
| `parse` | `kg_parse` | 同上 |
| `layout compute` | `kg_layout_compute` | 同上 |
| `analyze centrality` | `kg_analyze_centrality` | 同上 |
| `analyze pagerank` | `kg_analyze_pagerank` | 同上 |
| `analyze shortest-paths` | `kg_analyze_shortest_paths` | 同上 |
| `analyze components` | `kg_analyze_components` | 同上 |
| `analyze triangles` | `kg_analyze_triangles` | 同上 |
| `analyze topology` | `kg_analyze_topology` | 同上 |
| `embed nodes` | `kg_embed_nodes` | 同上 |
| `provider` | `kg_provider` | hub 自有（逃生口：`provider_id`/`capability_id`/`request`/`dry_run`） |
| `pipeline run` | `kg_pipeline_run` | hub 自有（`definition` 或 `definition_path`、`work_dir`、`resume`、`dry_run`、`provider`） |
| `pipeline validate` | `kg_pipeline_validate` | hub 自有（`definition` 或 `definition_path`、`provider`） |

- 命名规则稳定：`kg_` + semantic_id（空格/连字符→下划线），见
  `mcp.ToolName`。
- **能力 tool 的 inputSchema 就是 provider 发布的 `input_schema`
  原样**（probed 优先于 fallback 表，与 router.Resolve 同源）。hub
  只追加两个 hub-owned extra：`dry_run`（bool）与 `provider`
  （string）——CLI hub flag 的 MCP 形态，call 时在送进
  `router.Execute` 校验**之前**剥除，不污染 provider 的
  `additionalProperties: false`。
- 当前没有 provider 提供某能力时 schema 退化为 `{"type":"object"}`
  （hub 无法知道参数面）；call 时按 CLI 同一语义报
  `capability_not_found`。
- builtin tool 的 schema 是 hub 自有参数（不是 provider 参数），由
  hub 书写不违铁律 2。

## 3. 策略门注入

MCP 形态没有逐次调用的 `--allow-*` flag，门由 **server 启动配置**
供给，注入 `RunOptions.Gates` / `router.Execute`：

- 启动 flag：`kg-mcp --allow-network --allow-db-write ...`；
- 环境变量：`KG_ACME_ALLOW=network,data_egress`（逗号/空格/分号分隔，
  `*` 全开）；
- 两者 **OR 合并**；env 里出现未知 token → 启动即报错退出 2
  （fail-closed 配置错误必须响）。

门未开时 `tools/call` 返回 **结构化错误**：CallToolResult
`isError: true`，`structuredContent` 是 `status: "error"` 的
envelope（`error.code: "policy_denied"`），server 不退出、provider
进程不启动——与 CLI 的 envelope 语义一致。

## 4. 执行与输出映射

- `tools/call` → `router.Resolve` + `router.Execute`（能力 tool、
  `kg_provider`）或 `pipeline.Build` + `pipeline.Execute` /
  `RenderDryRun`（pipeline tool）——Phase 1/2 留的纯 Go API，无第二
  条执行路径。
- envelope（`kg.execution/v1` / `kg.pipeline.execution/v1`）原样成为
  `structuredContent`；`status: "error"` ↔ `isError: true`；`content`
  附一份 pretty JSON 文本给无 structured-content 能力的 client。
- **大 artifact 不内联**：artifact 始终是 path+checksum 引用，与 CLI
  完全一致；pipeline 的 artifact 同样落 `work_dir`（默认
  `kg-pipeline-<ts>/`，相对 server cwd）。
- `kg_pipeline_run` 接受内联 `definition`（对象，经
  `pipeline.ParseDefinition`）或 `definition_path`（经
  `pipeline.LoadDefinition`），二者必须恰好给一个。

## 5. provider 发现

与 CLI 同一入口：`router.DiscoverProviders(ctx, overrides)`（Phase 3
  从 `cmd/kg` 提取的共享函数，`findExecutable` 顺序不变）。
`--provider-bin ID=PATH` 为启动 flag（可重复）。provider 集在首次
`tools/list` / `tools/call` 时解析一次并缓存整个 server 生命周期。

## 6. 共享重构（Phase 3 顺带）

为让两个前端严格同源，提取了三处共享 API（行为不变）：

- `router.DiscoverProviders` ← 原 `cmd/kg` 的 `buildProviders`；
- `pipeline.ParseDefinition` ← 原 `LoadDefinition` 的解析半；
- `policy.ParseGates` / `Gates.Merge` ← env 允许表解析。

## 7. 客户端配置示例

Claude Code（`claude mcp add` 或 settings）与 Claude Desktop
（`claude_desktop_config.json`）同形：

```json
{
  "mcpServers": {
    "kg": {
      "command": "/Users/junix/projects/kg/kg-acme/kg-mcp",
      "args": ["--allow-network", "--allow-data-egress"],
      "env": {"KG_ACME_ALLOW": "network,data_egress"}
    }
  }
}
```

注入特定 provider 构建：args 里加
`"--provider-bin", "kg-extract=/Users/junix/projects/kg/kg-extract/target/debug/kg-extract"`。

## 8. 测试

- `internal/mcp/server_test.go`：帧级——initialize 握手（版本回显 /
  未知版本回最新）、notifications 容忍、`-32700` 后连接存活、
  `-32601`/`-32602`、tools/list 与 catalog 一一对应（同名断言 +
  description 同源断言）、kg_extract schema 逐字来自 probed provider
  / fallback 表、tools/call 经假 runner 端到端（hub extra 剥除断言）、
  policy_denied 结构化且不启动 provider、pipeline validate/run 门预检。
- `tests/mcp_e2e_test.go`：真 server 进程经管道对话——握手 +
  tools/list（假 provider 的 `file` 属性出现在 kg_extract schema）、
  tools/call kg_extract 端到端、无启动 flag 时 policy_denied 且 server
  存活、`KG_ACME_ALLOW=network` 开门、畸形 JSON 容错、
  `kg_pipeline_run` 三段链（parse→extract→store）artifact
  path+checksum 校验。
