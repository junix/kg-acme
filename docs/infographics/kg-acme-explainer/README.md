# kg-acme 可审计技术长图（explainer infographic）

一张宽 1200、滚动长页的浅色纸面技术长图，解释 kg-acme 引擎的能力中枢
机制：五职责内核、不可变能力快照、副作用四门、纯编排流水线与同源发布面。
页面显示层零代码细节（六禁门禁强制、正向对照 6/6），全部数字可溯源到
`data/` 冻结证据；file:line 锚点只进
[VERIFICATION.md](VERIFICATION.md) 验证层。

## 交付树

```
kg-acme-explainer/
├── index.html            滚动长页（零 JS、零外部请求，SVG 分层面板嵌入）
├── svg/                  11 张分层面板（程序化生成，1120 宽）
├── render/               位图三件 + 15 张分节 crops
│   ├── full-2x.png       2 倍全页（2400 × 19772 == 1200×9886 × dpr2）
│   ├── full-gray.png     灰度核查版
│   ├── thumb.png         360 宽缩略图
│   └── crops/            按 header/section/disc 的裁片
├── data/                 冻结证据 JSON（extract 产物 + 门禁豁免登记 + 指纹表）
├── tools/                工具链 extract → panels → build → render
│                         （+ fingerprint / vacuum）
├── README.md             本文
└── VERIFICATION.md       声明↔锚点对照、判例留痕、指纹表、偏差披露
```

## 环境变量（全部）

| 变量 | 必填 | 用途 |
|---|---|---|
| `KG_ACME_ROOT` | extract / build / vacuum 是 | 引擎仓根目录。extract 用它做只读冻结并校验冻结 HEAD；build 门禁用它**实时**重建六禁禁集（文件基名 / 逐字行集 / 标识符集）；vacuum 用它复跑全链。**缺失即 FATAL**（退出码 1，先例教训：真空段漏列引擎根会让复跑者拿到静默失败的旧门禁） |
| `IG_OUT` | 所有脚本 是 | 交付树根。**缺失即 FATAL**（无缺省值） |
| `IG_WORK` | extract / vacuum 是 | 固定 /tmp 工作目录（必须是 /tmp/ 绝对路径）。确定性策略的一部分：固定路径 + 固定 cwd + 封闭 HOME/PATH，使捕获输出逐字节可复现。**缺失即 FATAL** |
| `IG_CHROME_BIN` | render 否 | chrome-headless-shell 可执行文件；缺省按文档化规则取 `~/Library/Caches/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-*/chrome-headless-shell` 下最新者；找不到即 FATAL |
| `IG_DPR` | render 否 | 渲染像素比（默认 2） |
| `IG_SLICE` | render 否 | 长页切片高度 CSS px（默认 800） |
| `IG_RENDER_PORT` | render 否 | DevTools 端口（默认 18979） |

依赖：`python3`（含 Pillow）、`git`、`go`（仅 extract 用，离线构建）、
chrome-headless-shell（playwright 缓存内）、`svg-linter`
（`command -v svg-linter` 必须命中真二进制）。

## 重建命令链

引擎仓严格只读：所有脚本从 /tmp 平面拷贝运行（`PYTHONDONTWRITEBYTECODE=1`，
交付树内零 .pyc），引擎构建与测试都在 /tmp 工作拷贝里做。

```sh
TREE=<交付树绝对路径>
ENG=<引擎仓绝对路径>        # HEAD 必须是冻结 commit
RUN=$(mktemp -d /tmp/kgacme-ig-run.XXXX)
cd "$RUN"

# 1) 证据冻结（extract 校验冻结 HEAD + 干净 tracked 树 + 50 文件计数；
#    一次性层已存在则拒绝覆盖）
cp "$TREE/tools/extract.py" .
PYTHONDONTWRITEBYTECODE=1 KG_ACME_ROOT="$ENG" IG_OUT="$TREE" \
  IG_WORK=/tmp/kgacme-ig-run python3 extract.py

# 2) 面板生成（只读 data/*.json，写 svg/*.svg，不发明数字）
cp "$TREE/tools/panels.py" .
PYTHONDONTWRITEBYTECODE=1 IG_OUT="$TREE" python3 panels.py

# 3) 页面组装 + 六禁门禁（正向对照 6/6 必须咬住，产物 0 违规）
cp "$TREE/tools/build.py" .
PYTHONDONTWRITEBYTECODE=1 KG_ACME_ROOT="$ENG" IG_OUT="$TREE" python3 build.py

# 4) SVG 结构门禁（每张 rc=0 且 0 findings）
for f in "$TREE"/svg/*.svg; do
  out=$(svg-linter check --plain "$f"); rc=$?
  n=$(printf '%s\n' "$out" | awk -F'\t' '$1=="finding"' | wc -l)
  [ "$rc" -eq 0 ] && [ "$n" -eq 0 ] || { echo "GATE-FAIL $f"; exit 1; }
done

# 5) 位图渲染（CDP：逐片 scrollTo + scrollY 回读断言 → 视口截图 → 拼接；
#    全页高度 == CSS 高度 × dpr 硬断言；空快照/全白硬失败）
cp "$TREE/tools/render.py" .
PYTHONDONTWRITEBYTECODE=1 IG_OUT="$TREE" python3 render.py

# 6) 指纹表 + machine check
cp "$TREE/tools/fingerprint.py" .
PYTHONDONTWRITEBYTECODE=1 IG_OUT="$TREE" python3 fingerprint.py
PYTHONDONTWRITEBYTECODE=1 IG_OUT="$TREE" python3 fingerprint.py --check
```

## 证据分层（预先声明）

| 层 | 文件 | 判据 |
|---|---|---|
| 一次性冻结 | data/unit-tests.json | 计时类（真实测试运行墙钟与逐包秒数）。extract **拒绝覆盖**；真空断言逐字节不变 |
| 一次性冻结 | data/provenance.json | 生成时刻与工具版本。同上 |
| 确定性重建 | data/engine-contract.json | 固定 IG_WORK 路径 + 封闭 HOME/PATH + 常量假提供者输出 → 捕获字节可复现；双跑逐字节一致 |
| 确定性重建 | data/catalog-facts.json | 内置目录的纯函数 |
| 确定性重建 | data/repo-metrics.json | git ls-files + 行数统计的纯函数 |
| 确定性重建 | data/source-anchors.json | 冻结 HEAD 文本的纯函数（file:line 只进审计层） |
| 确定性重建 | data/display-exemptions.json | build.py 由同源输入写出 |
| 确定性重建 | data/fingerprints.json | fingerprint.py 对重建后树写出（不含时间戳） |
| 确定性重建 | index.html、svg/*.svg、render 三件 | panels/build/render 产物；真空判据见下 |

## 双跑真空（vacuum.py）

判据（预先写明，任何一条不满足即退出码 1）：

1. 引擎 HEAD == 冻结 commit，tracked 树干净（extract 启动即断言）；
2. 一次性层（unit-tests / provenance）**绝不重建**且逐字节不变；
3. 删除全部可再生产物（确定性 data 层 + index + svg + render）后全链
   重建：文本类**逐字节一致**；
4. 位图三件：首选**逐字节一致**；降级判据为像素零差（PIL）且剔除 PNG
   非像素辅助块后字节一致；
5. 六禁门禁、svg-linter 门禁（`command -v svg-linter` 真二进制，逐张
   rc=0 ∧ 0 findings）、渲染断言（1200 宽 / 逐片 scrollY 回读 / 高度
   等式）全部重过；
6. 指纹 machine check：无漂移且每个 sha 逐字出现在 VERIFICATION.md；
7. 残留普查：树内零 .pyc/__pycache__、封闭文件普查（意外文件即失败）、
   /tmp 工作目录已清除。

```sh
cd /tmp && cp "$TREE/tools/vacuum.py" . && \
  KG_ACME_ROOT="$ENG" IG_OUT="$TREE" IG_WORK=/tmp/kgacme-ig-run \
  python3 vacuum.py     # 预期输出末行 VACUUM-OK
```

本树实测结果：**VACUUM-OK**（文本与位图均逐字节一致，未动用降级判据），
见 VERIFICATION.md「双跑真空」。

## 敌意复核入口

- 门禁自检：`env -u KG_ACME_ROOT IG_OUT="$TREE" python3 build.py` →
  预期退出码 1；
- 指纹核对：`IG_OUT="$TREE" python3 fingerprint.py --check` → 任何漂移
  或未逐字登记即退出码 1；
- 六禁判例、C01–C30 声明↔锚点对照、指纹表与偏差披露：
  见 [VERIFICATION.md](VERIFICATION.md)。
