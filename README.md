# kg-acme

知识图谱能力 Hub。`kg` 只负责能力发现、协议、校验、策略和路由；KG 算法全部由 provider 工程实现。Provider 的 `describe --json` 是能力、参数和说明的权威来源，Hub 内的旧 CLI 表仅作无法自描述时的兼容 fallback。

## 命令分工

- `kg`：执行面，只接受 prefix-free 的 dotted Semantic ID。
- `kgctl`：管理面，负责 refresh、provider 诊断、能力检索、路由和 completion。
- `kg-mcp`：从同一份不可变快照发布 MCP tools。

安装时 `kgctl refresh` 生成 `~/Library/Caches/kg-acme/capability-snapshot.json`。`kg --help`、`list`、`--describe`、completion、MCP `tools/list` 和 `--dry-run` 只读此快照，不启动 provider，也不加载模型。真实原子能力执行会先完成参数和策略校验，再只复核并启动最终选中的 provider；pipeline 会在完整预检后仅复核计划实际使用的 provider。

## 使用

```sh
kg --help
kg list --level 1
kg list --prefix analyze --level 0 --tree
kg analyze.pagerank --describe
kg analyze.pagerank --params '{"edges":[["a","b"]]}' --dry-run --json

kg pipeline.validate pipeline.json --json
kg pipeline.run pipeline.json --dry-run --json

kgctl refresh
kgctl capabilities list
kgctl capabilities list --all
kgctl capabilities search community
kgctl providers doctor --all
kgctl route explain analyze.pagerank
```

原子 `--describe` 返回一个 schema 对象；group（例如 `kg analyze --describe`）返回其后代 schema 数组。`list --level 0` 表示展开到最大深度。默认 capability list 只有 Semantic ID 和 Description 两列；只有 `--all` 才显示可用状态。

执行参数既可使用 provider 自描述的 convenience flags，也可由 `--params '<json>'` 或 `--params @request.json` 一次传入。默认拒绝 network、data egress、model download 和 database write，按需显式添加对应的 `--allow-*`。

## Source 追溯

能力 help/describe 同时给出 ACME integration path 和 provider implementation path。第三方 provider 应在 manifest 中发布：

```json
{"source":{"local_code_path":"/path/to/provider/checkout"}}
```

## 构建

```sh
just build
just test
just check
just install
```

`just install` 安装 `kg`、`kgctl`、`kg-mcp` 到 `~/sync/<os>-<arch>-bin/` 并刷新快照。
