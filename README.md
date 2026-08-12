# kg-acme

知识图谱工具链的 **capability hub**——acme 家族模式在 KG 工具集上的落地。
一个 `kg` CLI，把实现了 `kg.provider/v1` 协议的 provider（以及存量 legacy
CLI）组织成统一的能力目录：发现、路由、策略治理、执行。

## 两条铁律

1. **hub 只做集成**（发现/协议/catalog/策略/路由），绝不自实现任何 KG 算法。
2. **hub 不写死 provider 的选项/枚举**——provider `describe --json` 自描述
   （cli_spec + input_schema），hub 照此渲染 argv/校验/呈现；hub 内置的
   兼容桥 argv 表只是 fallback。

设计文档见 `spec/`（00-overview 起）。

## 构建与测试

```sh
just build   # go build ./... && go build -o kg ./cmd/kg && go build -o kg-mcp ./cmd/kg-mcp
just test    # go test ./...
just vet     # go vet ./...
just check   # vet + test + build
```

## 命令面

```sh
kg list [--json]                        # 发现的 provider 与能力表
kg describe <provider> [--json]         # provider 自描述（或 fallback 表）
kg extract [--file f] [--backend b] [--mock-response r] ...   # extract.entities_relations
kg dedup                                # resolve.coref（graph-in，经协议 invoke）
kg communities [hierarchy|summaries|semantic]   # detect.communities[_hierarchy|_semantic] / summarize.communities
kg store                                # store.triples（graph-in，经协议 invoke）
kg ask --dataset d --question q [--mode local|global]         # retrieve.ask
kg parse <sidecar.json> [--backend mock] ...                  # parse.multimodal
kg layout compute --edges '["a","b"]' --algorithm circular    # layout.compute
kg analyze centrality --edges '["a","b"]' --method pagerank   # analyze.centrality
kg analyze pagerank --edges '["a","b"]' --edges '["b","a"]' --directed  # analyze.pagerank
kg analyze shortest-paths --edges '["a","b",2]' --source a    # analyze.shortest_paths
kg analyze components --edges '["a","b"]' --kind weak         # analyze.components
kg analyze triangles --edges '["a","b"]'                      # analyze.triangles
kg analyze topology --edges '["a","b"]' --operation bridges   # analyze.topology
kg embed nodes --edges '["a","b"]' [--dimensions 8] ...       # embed.nodes
kg provider <id> <capability_id> --request <file|->     # 长尾逃生口
kg pipeline run <def.json> [--dry-run] [--work-dir d | --resume d]   # kg.pipeline/v1 流水线
kg pipeline validate <def.json>                       # 只校验不执行
```

pipeline（Phase 2，详见 `spec/05-pipeline.md`）：声明式 `kg.pipeline/v1`
定义把多个能力串成 DAG——stage 间以 artifact 类型边衔接（定义期校验，
不兼容报 `incompatible_stage_edge`），拓扑序执行，每 stage 走
router（probed 优先 + 策略门）；策略门按全部 stage 副作用并集预检
fail fast；artifact 统一落 `--work-dir`（默认 `kg-pipeline-<ts>/`），
`--resume <dir>` 跳过已完成 stage（checksum 复验）；`optional: true`
stage 失败跳过。`--json` 输出恰好一个 `kg.pipeline.execution/v1`
envelope。

catalog 稳定命令映射的是 **provider 发布的能力命名空间**（`extract.*` /
`detect.*` / `summarize.*` / `resolve.*` / `store.*` / `retrieve.*` /
`parse.*` / `layout.*` / `analyze.*` / `embed.*`）；能力命令的参数面完全
来自 provider 的 cli_spec（或 fallback 桥表），hub 不自有任何 provider
flag。graph-in 能力（输入是图谱文档）经协议 `invoke --request -` 调用，
也可用逃生口直接送请求。array 类参数支持 JSON 值
（`--edges '["a","b"]'` 追加一个结构化元素，见 spec/02）。

hub 标志：`--json`（stdout 恰好一个 kg.execution/v1 envelope）、
`--dry-run`（零副作用执行计划）、`--allow-network` / `--allow-data-egress` /
`--allow-model-download` / `--allow-db-write`（策略门，默认全拒）、
`--provider <id>`、`--provider-bin ID=PATH`、`--work-dir <dir>` /
`--resume <dir>`（pipeline 专用）。

## kg-mcp：MCP server 形态（Phase 3，详见 spec/06）

`kg-mcp` 把同一 hub 以 MCP stdio server 展开：每个 catalog 命令是一个
tool（`kg_extract` … `kg_pipeline_run`），能力 tool 的 inputSchema 就是
provider 发布的 `input_schema` 原样；策略门由启动配置供给
（`--allow-*` flag 或 `KG_ACME_ALLOW` env，OR 合并）；envelope 即
structured content，policy_denied 等失败是结构化 `isError: true`，
server 不退出。

```sh
kg-mcp --allow-network --provider-bin kg-extract=/path/to/kg-extract
```

Claude Code / Claude Desktop 配置：

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

## 示例

```sh
# 零副作用预览：渲染 argv 与策略判定，不执行
kg extract --file doc.md --dry-run --json

# 默认策略门全拒 → policy_denied
kg extract --file doc.md --json

# 显式放行后执行
kg extract --file doc.md --json --allow-network --allow-data-egress

# 强制走 legacy 桥
kg extract --file doc.md --provider kg-extract --allow-network --allow-data-egress

# 逃生口：raw 协议调用
echo '{"file":"doc.md"}' | kg provider kg-provider-fake extract.entities_relations --request - --json --allow-network
```

## provider 发现顺序

1. `--provider-bin ID=PATH` 显式覆盖
2. `~/sync/<os>-<arch>-bin/`（darwin 上兼认 `macos-<arch>-bin` 别名）
3. `~/sync/bin/`
4. `PATH`
5. `PATH` 中 `kg-provider-*`

内置 fallback 桥（按 provider 发布命名空间挂键，只桥接有真实 argv
形态的能力）：`kg-extract`（extract.entities_relations）、
`kg-mm`（parse.multimodal）、`ygr`（retrieve.ask）。provider 一旦实现
`kg.provider/v1` 自描述，自动切换为协议模式（probed 优先）；provider
自描述与 fallback 表漂移时发射 `cli_spec differs ...` diagnostic。

已知协议原生 provider（按名发现，无 fallback 桥，只能 probe 后使用，
`router.ProtocolNativeBins`）：`kg-layout`（layout.compute /
layout.draw，布局算法）、`graph-kg`（analyze.centrality /
detect.communities_semantic / embed.nodes，Python 图分析）、`kg-algorithms`
（analyze.pagerank / shortest_paths / components / triangles / topology，
Rust 图算法）。Python provider 由 `~/sync/bin/` wrapper 进入发现顺序；
Rust provider 安装到 `~/sync/<os>-<arch>-bin/`。

## 布局

```
cmd/kg/            CLI 入口
cmd/kg-mcp/        MCP server（stdio）入口
internal/
  protocol/        kg.provider/v1 + kg.execution/v1 类型与版本协商
  catalog/         稳定命令目录（go:embed catalog.json + Load 校验）
  discover/        可执行文件发现 + describe/available probe
  router/          capability 解析、argv→input、执行（协议/桥两路）+ 共享 provider 集装配
  policy/          side-effect 策略门
  pipeline/        kg.pipeline/v1 定义、DAG 计划/校验、执行与 resume
  mcp/             stdio JSON-RPC 帧循环 + catalog→tool 同源展开
  bridge/          兼容桥 fallback argv 表 + renderArgv
  schema/          JSON Schema 校验（santhosh-tekuri/jsonschema）
spec/              设计文档
tests/             端到端测试（假 provider shell 脚本 + kg-mcp 真进程对话）
```
