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
just build   # go build ./... && go build -o kg ./cmd/kg
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
kg communities [hierarchy|summaries]    # detect.communities[_hierarchy] / summarize.communities
kg store                                # store.triples（graph-in，经协议 invoke）
kg ask --dataset d --question q [--mode local|global]         # retrieve.ask
kg parse <sidecar.json> [--backend mock] ...                  # parse.multimodal
kg provider <id> <capability_id> --request <file|->     # 长尾逃生口
kg pipeline ...                         # stub（Phase 2）
```

catalog 稳定命令映射的是 **provider 发布的能力命名空间**（`extract.*` /
`detect.*` / `summarize.*` / `resolve.*` / `store.*` / `retrieve.*` /
`parse.*`）；能力命令的参数面完全来自 provider 的 cli_spec（或 fallback
桥表），hub 不自有任何 provider flag。graph-in 能力（输入是图谱文档）
经协议 `invoke --request -` 调用，也可用逃生口直接送请求。

hub 标志：`--json`（stdout 恰好一个 kg.execution/v1 envelope）、
`--dry-run`（零副作用执行计划）、`--allow-network` / `--allow-data-egress` /
`--allow-model-download` / `--allow-db-write`（策略门，默认全拒）、
`--provider <id>`、`--provider-bin ID=PATH`。

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

## 布局

```
cmd/kg/            CLI 入口
internal/
  protocol/        kg.provider/v1 + kg.execution/v1 类型与版本协商
  catalog/         稳定命令目录（go:embed catalog.json + Load 校验）
  discover/        可执行文件发现 + describe/available probe
  router/          capability 解析、argv→input、执行（协议/桥两路）
  policy/          side-effect 策略门
  bridge/          兼容桥 fallback argv 表 + renderArgv
  schema/          JSON Schema 校验（santhosh-tekuri/jsonschema）
spec/              设计文档
tests/             端到端测试（假 provider shell 脚本）
```
