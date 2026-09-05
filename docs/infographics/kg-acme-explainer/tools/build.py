#!/usr/bin/env python3
"""Page assembler + six-ban gate for the kg-acme explainer infographic.

Writes index.html (1200 CSS px, zero JS, zero CDN, zero external requests,
Chinese, blue chrome) from data/*.json + svg/*.svg, then runs the six-ban
gate over the display layer (index.html + svg/*.svg) with ban sets built
LIVE from the engine repo, plus six positive controls that must be caught
6/6. Exemptions (ban④ public contract vocabulary + frozen-data strings)
are recorded to data/display-exemptions.json.

Six bans (display layer only; data/, VERIFICATION.md, README are the audit
layer and unrestricted):
  ① engine source file basenames (git ls-files, live)
  ② file:line / line numbers / ranges
  ③ verbatim source excerpts >= 25 chars (engine .go/.md lines)
  ④ engine-declared identifiers (public contract vocabulary exempt:
     CLI verbs & flags, JSON contract keys/values, protocol ids, error
     codes, ecosystem names — recorded in display-exemptions.json)
  ⑤ engine internal paths
  ⑥ generator names & rebuild commands

Usage (from a /tmp flat copy):

    KG_ACME_ROOT=/path/to/kg-acme IG_OUT=/path/to/tree python3 build.py

Environment (ALL required; missing = hard fail, no silent defaults — this
is itself gate-tested: `env -u KG_ACME_ROOT ... build.py` must exit 1).
"""

import json
import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = os.environ.get("KG_ACME_ROOT") or ""
OUT = Path(os.environ.get("IG_OUT") or "")
for name, val in [("KG_ACME_ROOT", ROOT), ("IG_OUT", OUT)]:
    if not val:
        print(f"build: FATAL: {name} not set (required, no default)",
              file=sys.stderr)
        sys.exit(1)
ROOT = Path(ROOT)
DATA = OUT / "data"
SVGD = OUT / "svg"


def die(msg):
    print(f"build: FATAL: {msg}", file=sys.stderr)
    sys.exit(1)


def load(name):
    return json.loads((DATA / name).read_text(encoding="utf-8"))


EC = load("engine-contract.json")
UT = load("unit-tests.json")
RM = load("repo-metrics.json")
CF = load("catalog-facts.json")
PROV = load("provenance.json")

HEAD_SHORT = PROV["engine_head"][:7]
CLAIM_IDS = [f"C{i:02d}" for i in range(1, 31)]

# ============================================================ page content
SECTIONS = [
    ("p0-hero.svg", "总览",
     "两条铁律定边界：中枢只做集成，参数面永远来自 provider 自描述。"
     "三个二进制入口共享同一份不可变能力快照。"),
    ("p1-architecture.svg", "架构：五职责内核",
     "发现、协议、目录、策略、路由五个职责构成内核；provider 分协议原生与"
     "遗留命令行两侧接入，路由排序确定。"),
    ("p2-snapshot.svg", "数据流：一份快照两种纪律",
     "只读面永不启动 provider（日志实测为零）；执行面只对最终选中者做一次"
     "复核与调用。"),
    ("p3-cli.svg", "命令面：动词与稳定目录",
     "执行面、管理面、发布面各持一段动词；内置目录是唯一长期稳定契约，"
     "加载期强校验。"),
    ("p4-protocol.svg", "协议：三个动词",
     "自描述、依赖探测、调用三个动词承载全部 provider 通信；版本协商与"
     "清单校验把失败分成两类。"),
    ("p5-policy.svg", "行为门禁：默认全拒",
     "四类副作用对应四把旗标；未知副作用无门可开；干跑渲染完整执行计划"
     "而零副作用。"),
    ("p6-errors.svg", "错误处理：九码与单信封",
     "机器错误码集合固定；标准输出永远恰好一个信封；失败契约逐例实测"
     "冻结。"),
    ("p7-pipeline.svg", "流水线：纯编排",
     "每一步都是一次完整路由调用；类型边在计划期校验；工作目录自包含；"
     "校验和一致即复用。"),
    ("p8-mcp.svg", "发布面：同源工具清单",
     "工具清单由同一份快照运行时派生；策略门由启动配置注入；帧级错误"
     "语义固定。"),
    ("p9-quality.svg", "工程事实",
     "规模先枚举后求和；两层测试网全绿冻结；全部数字锚定证据文件。"),
    ("p10-contract.svg", "契约实测案例",
     "五个端到端案例全部由真二进制加假提供者跑出并冻结，可复算、可核查。"),
]

CSS = """
:root{--paper:#F4F7FB;--card:#FFFFFF;--ink:#1B2733;--sub:#46586B;
--mut:#7C8FA3;--line:#D8E2EC;--accent:#1D5FBF;--deep:#0E3A75;
--chip:#E3EEFA}
*{margin:0;padding:0;box-sizing:border-box}
body{background:var(--paper);color:var(--ink);font-family:'PingFang SC',
'Hiragino Sans GB','Microsoft YaHei',sans-serif}
.wrap{width:1200px;margin:0 auto}
header{background:linear-gradient(180deg,#0E3A75 0%,#1D5FBF 100%);
color:#fff;padding:64px 0 44px}
header .kicker{font-size:15px;letter-spacing:.12em;opacity:.85}
header h1{font-size:40px;line-height:1.25;margin:14px 0 12px;font-weight:700}
header p.sub{font-size:17px;opacity:.9;line-height:1.7;max-width:880px;
padding:0 40px}
.metastrip{display:flex;gap:12px;margin:26px 40px 0;flex-wrap:wrap}
.metastrip .m{background:rgba(255,255,255,.13);border:1px solid rgba(255,
255,255,.28);border-radius:9px;padding:7px 14px;font-size:13px}
section{padding:34px 40px 8px}
.sec-head{display:flex;align-items:baseline;gap:14px;margin-bottom:6px}
.sec-no{font-size:14px;color:var(--accent);font-weight:700;letter-spacing:.1em}
h2{font-size:26px;font-weight:700}
p.lead{color:var(--sub);font-size:15px;line-height:1.8;margin:8px 0 16px;
max-width:980px}
.panel{background:var(--card);border:1px solid var(--line);border-radius:14px;
padding:38px}
.panel img{display:block;width:1042px;height:auto;margin:0 auto}
.disc{background:var(--card);border:1px solid var(--line);border-radius:14px;
margin:26px 40px 0;padding:22px 28px}
.disc h3{font-size:17px;color:var(--deep);margin-bottom:8px}
.disc p{font-size:13.5px;color:var(--sub);line-height:1.85}
footer{padding:40px 0;background:var(--paper)}
.foot-card{background:var(--card);border:1px solid var(--line);border-radius:
14px;padding:26px 30px;display:flex;gap:28px;justify-content:space-between;
flex-wrap:wrap}
.foot-card .col{max-width:520px}
.foot-card h4{font-size:15px;color:var(--deep);margin-bottom:6px}
.foot-card p{font-size:13px;color:var(--sub);line-height:1.8}
"""

parts = []
parts.append(
    "<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n"
    "<meta charset=\"utf-8\">\n"
    "<meta name=\"viewport\" content=\"width=1200\">\n"
    "<title>kg-acme 技术长图：知识图谱能力中枢</title>\n"
    f"<style>{CSS}</style>\n</head>\n<body>")
parts.append(
    '<header><div class="wrap">'
    '<div class="kicker">可审计技术长图 · 全部数字锚定冻结证据</div>'
    "<h1>kg-acme：知识图谱能力中枢</h1>"
    '<p class="sub">中枢只做集成：发现、协议、目录、策略、路由五件事；'
    "图谱算法全部住在 provider 里。本页解释三条入口、一份不可变快照、"
    "四把策略门与一条纯编排流水线。</p>"
    '<div class="metastrip">'
    f'<div class="m">引擎提交 {HEAD_SHORT}（冻结）</div>'
    f'<div class="m">冻结证据 6 份 JSON</div>'
    f'<div class="m">面板 11 张 · 声明 30 条</div>'
    f'<div class="m">测试 {UT["total_passed"]} 通过 / '
    f'{UT["total_failed"]} 失败（一次冻结运行）</div>'
    f'<div class="m">跟踪文件 {RM["tracked_total"]} 个</div>'
    "</div></div></header>")
parts.append('<div class="wrap">')
parts.append(
    '<section style="padding-top:34px">'
    '<div class="sec-head"><span class="sec-no">导读</span></div>'
    '<p class="lead">这张长图按「总览 → 架构 → 命令面 → 行为与门禁 → '
    "错误处理 → 流水线与发布面 → 工程事实 → 契约实测」组织。每张面板右下角"
    "的编号是声明编号：30 条声明逐条登记在交付目录的验证层，每条都给出"
    "冻结证据与源锚点，任何一个数字都可以被独立复算。显示层不放任何代码"
    "细节——那是审计层的事。</p></section>")
for i, (svg_name, title, lead) in enumerate(SECTIONS):
    img = SVGD / svg_name
    if not img.exists():
        die(f"panel missing: svg/{svg_name} (run panels.py first)")
    parts.append(
        f'<section id="s{i}"><div class="sec-head">'
        f'<span class="sec-no">{i + 1:02d}</span><h2>{title}</h2></div>'
        f'<p class="lead">{lead}</p>'
        f'<div class="panel"><img src="svg/{svg_name}" '
        f'alt="{title}面板" width="1120"></div></section>')
parts.append(
    '<div class="disc"><h3>本页纪律</h3>'
    "<p>零脚本、零外部请求：整页仅引用同目录面板文件，无任何网络依赖。"
    "显示层执行六条禁令：不出现引擎源码文件名、代码坐标、逐字摘录、内部"
    "标识符、内部路径与任何生成器或重建命令；豁免词表（公开契约词汇与"
    "冻结数据字符串）登记在交付目录证据层。位图三件（两倍全页、灰度版、"
    "缩略图）与逐节裁片由交付目录渲染产物提供，渲染断言含全页高度等式。"
    "</p></div>")
parts.append("</div>")
parts.append(
    "<footer><div class=\"wrap\"><div class=\"foot-card\">"
    '<div class="col"><h4>如何核查本页</h4>'
    "<p>声明编号 C01–C30 逐条对应验证层登记表：每条声明给出冻结证据文件、"
    "字段路径与引擎源锚点，并附全部产物的指纹表与已知偏差披露。验证与"
    "重建的完整说明在交付目录的验证与工具文档里。</p></div>"
    '<div class="col"><h4>证据分层</h4>'
    "<p>一次性冻结层（计时类，绝不重测）与确定性重建层（双跑逐字节一致）"
    "在证据登记簿中预先声明；行为证据全部来自真二进制对假提供者的实测，"
    "零真实网络、零模型调用。</p></div>"
    "</div></div></footer>")
parts.append("</body>\n</html>")
page = OUT / "index.html"
page.write_text("\n".join(parts) + "\n", encoding="utf-8")
print(f"build: wrote index.html ({page.stat().st_size:,} bytes)")

# ============================================================ six-ban gate
tracked = [l.strip() for l in subprocess.run(
    ["git", "-C", str(ROOT), "ls-files"], capture_output=True,
    text=True).stdout.splitlines() if l.strip()]
if not tracked:
    die("could not read engine file list (git ls-files)")

display = [(OUT / "index.html").read_text(encoding="utf-8")]
for f in sorted(SVGD.glob("*.svg")):
    display.append(f.read_text(encoding="utf-8"))
display_text = "\n".join(display)
display_flat = re.sub(r"\s+", " ", display_text)

# ---- ban set ① basenames + ⑤ paths (live) --------------------------------
basenames = sorted({p.rsplit("/", 1)[-1] for p in tracked})
path_re = re.compile(r"(?:^|[\s\"'>(])(cmd|internal|spec|tests|docs|pkg)/"
                     r"[\w./-]+")

# ---- ban set ③ verbatim lines (>=25 chars, engine .go/.md) ----------------
verbatim = []
for t in tracked:
    if t.endswith(".go") or t.endswith(".md"):
        for line in (ROOT / t).read_text(encoding="utf-8",
                                         errors="replace").splitlines():
            norm = re.sub(r"\s+", " ", line).strip()
            if len(norm) >= 25:
                verbatim.append(norm)
verbatim = sorted(set(verbatim))

# ---- ban set ④ identifiers (Go declared names + Capitalized words) --------
declared = set()
cap_words = set()
decl_re = re.compile(
    r"\b(?:func|type|const|var)\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)")
cap_re = re.compile(r"\b([A-Z][A-Za-z0-9]{3,})\b")
for t in tracked:
    if not t.endswith(".go"):
        continue
    text = (ROOT / t).read_text(encoding="utf-8", errors="replace")
    declared.update(decl_re.findall(text))
    cap_words.update(cap_re.findall(text))
ban_idents = {w for w in (declared | cap_words)
              if len(w) >= 3 and w not in {"The", "This", "That", "These",
                                           "Those", "With", "From", "Then",
                                           "When", "Will", "Your", "They",
                                           "There", "Their", "What", "Also",
                                           "Some", "Must", "Note", "Only",
                                           "Each", "Both", "Even", "Into",
                                           "Over", "Such", "Same", "Once",
                                           "True", "False", "TODO", "FIXME",
                                           "GOGC", "GOOS", "GOARCH"}}

# ---- ban set ② file:line / line numbers -----------------------------------
fileline_re = re.compile(
    r"[\w.-]+\.(?:go|md|json|mod|sum|txt|sh|py)\s*[:：]\s*\d+")
lineno_re = re.compile(r"(?:第\s*\d+\s*行|line\s+\d+|\bL\d+\b)")

# ---- ban set ⑥ generators & rebuild commands ------------------------------
gen_tokens = ["extract.py", "panels.py", "build.py", "render.py",
              "vacuum.py", "fingerprint.py", "python", "python3", "pip",
              "pip3", "node", "npm", "cargo", "rustc", "svg-linter",
              "svg_linter", "chrome", "chromium", "headless", "playwright",
              "chrome-headless-shell", "justfile", "shasum", "sha256sum",
              "go build", "go test", "git archive", "gofmt", "websocket",
              "websockets"]

# ---- exemptions ------------------------------------------------------------
WHITELIST = {
    "cli_verbs": ["kg", "kgctl", "kg-mcp", "refresh", "providers",
                  "capabilities", "route", "completion", "list", "describe",
                  "version", "help", "ping", "initialize", "call", "run",
                  "validate", "explain", "tree"],
    "cli_flags": ["params", "json", "dry", "run", "dry_run", "prefix",
                  "level", "allow", "network", "data", "egress", "model",
                  "download", "db", "write", "provider", "bin", "work",
                  "dir", "resume", "all"],
    "protocol_verbs": ["describe", "available", "invoke", "request"],
    "contract_keys": ["schema", "version", "capability", "id", "provider",
                      "side", "effects", "denied", "would", "execute",
                      "status", "result", "input", "output", "artifacts",
                      "checksum", "tools", "error", "ok", "message", "code",
                      "argv", "out", "value", "probed", "envelope", "stages",
                      "stage", "pipeline"],
    "error_codes": ["unsupported_schema_version", "malformed_manifest",
                    "capability_not_found", "provider_not_found",
                    "policy_denied", "invalid_input", "invocation_failed",
                    "invalid_pipeline", "incompatible_stage_edge", "error"],
    "schema_ids": ["kg", "error", "v1", "execution", "snapshot", "pipeline",
                   "provider"],
    "side_effects": ["network", "data_egress", "downloads_models",
                     "writes_db"],
    "ecosystem": ["JSON", "MCP", "CLI", "Go", "git", "shell", "stdio",
                  "jsonrpc", "hub", "snapshot", "catalog", "pipeline",
                  "provider", "providers", "capability", "capabilities",
                  "semantic", "artifact", "dry", "run", "net", "echo",
                  "test", "fake", "ver", "ugly", "bad", "time"],
    "published_namespaces": CF["published_namespaces"],
    "frozen_cli_lines": [l.split()[0].lstrip("-").replace("-", "_")
                         for l in EC["help_contract"]["global_flag_lines"]],
}
exempt_words = set()
for words in WHITELIST.values():
    exempt_words.update(words)

# frozen-data strings: any identifier token appearing in a frozen JSON
# string value is displayable (the page interpolates frozen evidence).
# display-exemptions.json and fingerprints.json are build OUTPUTS, not
# frozen-evidence inputs; feeding them back here would make the exemption
# registry depend on whether a stale copy existed (2-byte drift caught by
# the first vacuum run) — they are excluded from the walk.
BUILD_OUTPUTS = {"display-exemptions.json", "fingerprints.json"}
data_tokens = set()
for jf in sorted(DATA.glob("*.json")):
    if jf.name in BUILD_OUTPUTS:
        continue
    doc = json.loads(jf.read_text(encoding="utf-8"))

    def walk(node):
        if isinstance(node, dict):
            for v in node.values():
                walk(v)
        elif isinstance(node, list):
            for v in node:
                walk(v)
        elif isinstance(node, str):
            data_tokens.update(re.findall(r"[A-Za-z_][A-Za-z0-9_]+", node))
    walk(doc)
exempt_words |= data_tokens

VERBATIM_EXEMPT = []  # none anticipated; any addition must carry a reason

violations = []


def check_1():
    hits = [b for b in basenames if b in display_text]
    if hits:
        violations.append(("ban1-basenames", hits[:10]))


def check_2():
    hits = sorted(set(fileline_re.findall(display_flat)) |
                  set(lineno_re.findall(display_flat)))
    if hits:
        violations.append(("ban2-file-line", [str(h) for h in hits[:10]]))


def check_3():
    hits = [v for v in verbatim
            if v in display_flat and v not in VERBATIM_EXEMPT]
    if hits:
        violations.append(("ban3-verbatim", hits[:10]))


def check_4():
    page_tokens = set(re.findall(r"[A-Za-z_][A-Za-z0-9_]+", display_text))
    hits = sorted(page_tokens & ban_idents - exempt_words)
    if hits:
        violations.append(("ban4-identifiers", hits[:25]))


def check_5():
    hits = sorted(set(m.group(0).strip() for m in
                      path_re.finditer(display_flat)))
    if hits:
        violations.append(("ban5-paths", hits[:10]))


def check_6():
    low = display_flat.lower()
    hits = [g for g in gen_tokens
            if re.search(r"(?<![A-Za-z0-9_-])%s(?![A-Za-z0-9_-])" %
                         re.escape(g.lower()), low)]
    if hits:
        violations.append(("ban6-generators", hits[:10]))


for chk in (check_1, check_2, check_3, check_4, check_5, check_6):
    chk()

# ---- positive controls: six self-made violations must be caught 6/6 -------
spec_file = next(t for t in tracked if t.endswith("00-overview.md"))
spec_lines = [re.sub(r"\s+", " ", l).strip() for l in
              (ROOT / spec_file).read_text(encoding="utf-8").splitlines()]
verbatim_control = next(l for l in spec_lines if len(l) >= 40)
go_file = next(t for t in tracked if t.endswith(".go"))
go_text = (ROOT / go_file).read_text(encoding="utf-8")
ident_control = next(w for w in sorted(ban_idents, reverse=True)
                     if w not in exempt_words and re.search(
                         r"\bfunc %s\b" % re.escape(w), go_text) is None and
                     re.fullmatch(r"[A-Z]\w+", w))
controls = [
    ("ban1", f"看看 {tracked[10].rsplit('/', 1)[-1]} 这个文件"),
    ("ban2", f"实现在 {go_file.rsplit('/', 1)[-1]}:42 附近"),
    ("ban3", verbatim_control),
    ("ban4", f"函数 {ident_control} 负责路由"),
    ("ban5", f"路径 {tracked[20]}"),
    ("ban6", "重建命令 python3 render.py"),
]


def detect(text):
    flat = re.sub(r"\s+", " ", text)
    caught = []
    if any(b in text for b in basenames):
        caught.append("ban1")
    if fileline_re.search(flat) or lineno_re.search(flat):
        caught.append("ban2")
    if any(v in flat for v in verbatim if v not in VERBATIM_EXEMPT):
        caught.append("ban3")
    toks = set(re.findall(r"[A-Za-z_][A-Za-z0-9_]+", text))
    if toks & ban_idents - exempt_words:
        caught.append("ban4")
    if path_re.search(flat):
        caught.append("ban5")
    low = flat.lower()
    if any(re.search(r"(?<![A-Za-z0-9_-])%s(?![A-Za-z0-9_-])" %
                     re.escape(g.lower()), low) for g in gen_tokens):
        caught.append("ban6")
    return caught


control_results = []
for expect, ctrl in controls:
    caught = detect(ctrl)
    ok = expect in caught
    control_results.append({"expect": expect, "text": ctrl[:80],
                            "caught": caught, "pass": ok})
n_ok = sum(1 for c in control_results if c["pass"])
print(f"build: positive controls {n_ok}/6")
for c in control_results:
    print(f"  {c['expect']}: {'PASS' if c['pass'] else 'FAIL'} "
          f"caught={c['caught']}")

# ---- self-containment assertions ------------------------------------------
sc = []
if re.search(r"<script", display[0], re.I):
    sc.append("index.html contains <script>")
if re.search(r"(?:src|href)\s*=\s*[\"'](?:https?:)?//", display[0], re.I):
    sc.append("protocol-relative or absolute external URL")
if re.search(r"(?:https?:)?//[a-z0-9.-]+\.", display[0], re.I):
    sc.append("external host reference")
if re.search(r"\son[a-z]+\s*=", display[0], re.I):
    sc.append("inline event handler")
if re.search(r"<link|@import|<iframe|<object|<embed", display[0], re.I):
    sc.append("external resource element")
for m in re.finditer(r'(?:src|href)="([^"]+)"', display[0]):
    target = OUT / m.group(1)
    if not target.exists():
        sc.append(f"dangling reference {m.group(1)}")
if sc:
    violations.append(("self-containment", sc))
widths = re.findall(r"<img ", display[0])
if len(widths) != len(SECTIONS) or "width:1042px" not in display[0]:
    violations.append(("panel-width",
                       [f"imgs={len(widths)} sections={len(SECTIONS)}"]))

# ---- claim coverage --------------------------------------------------------
page_claims = sorted(set(re.findall(r"\bC\d{2}\b", display_text)))
expected = CLAIM_IDS
if page_claims != expected:
    missing = [c for c in expected if c not in page_claims]
    extra = [c for c in page_claims if c not in expected]
    violations.append(("claim-coverage",
                       [f"missing={missing}", f"extra={extra}"]))

# ---- record exemptions (deterministic) -------------------------------------
(DATA / "display-exemptions.json").write_text(
    json.dumps({
        "note": "display-layer exemption registry for ban④; ban sets "
                "themselves are rebuilt live from the engine repo by "
                "build.py on every run",
        "whitelist_categories": {
            k: sorted(v) for k, v in sorted(WHITELIST.items())},
        "frozen_data_token_count": len(data_tokens & exempt_words),
        "verbatim_exempt": VERBATIM_EXEMPT,
        "positive_controls": control_results,
        "gate_result_at_write": "pending-final-check",
    }, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print("build: wrote data/display-exemptions.json")

if n_ok != 6:
    violations.append(("positive-controls", [f"{n_ok}/6 caught"]))
if violations:
    print("build: SIX-BAN GATE FAILED:", file=sys.stderr)
    for kind, items in violations:
        print(f"  {kind}: {items}", file=sys.stderr)
    sys.exit(1)
print("build: SIX-BAN GATE PASS (0 violations, controls 6/6, "
      "self-contained, claims C01–C30 covered)")
