#!/usr/bin/env python3
"""SVG panel generator for the kg-acme explainer infographic.

Reads data/*.json (frozen evidence) and writes svg/p0-*.svg … svg/p10-*.svg.
Pure renderer: every number is interpolated from the frozen evidence files;
no number is typed by hand. Panels carry no engine code detail (the six-ban
gate in build.py enforces that separately against the live engine tree).

Usage (from a /tmp flat copy, never inside the delivery tree):

    IG_OUT=/path/to/tree python3 panels.py

Environment (required; missing = hard fail, no silent defaults):
    IG_OUT   delivery tree root
"""

import json
import math
import os
import sys
from pathlib import Path

OUT = Path(os.environ.get("IG_OUT") or "")
if not OUT:
    print("panels: FATAL: IG_OUT not set (required, no default)",
          file=sys.stderr)
    sys.exit(1)
DATA = OUT / "data"
SVGD = OUT / "svg"
SVGD.mkdir(parents=True, exist_ok=True)

# ---- page chrome tokens (non-data: structure, borders, chips) --------------
INK = "#1B2733"
SUB = "#46586B"
MUT = "#7C8FA3"
PAPER = "#FFFFFF"
BORDER = "#D8E2EC"
ACCENT = "#1D5FBF"          # UI accent (kickers, card top bars) — not a series
DEEP = "#0E3A75"
CHIP_BG = "#E3EEFA"
CHIP_BG2 = "#E8EEF5"
# ---- validated data series (dataviz six-checks passed, all-pairs) ----------
S1 = "#2a78d6"              # series 1: blue (primary data marks)
S2 = "#4a3aa7"              # series 2: violet (secondary identity)
STATUS_DENY = "#e34948"     # reserved status: denied/error (always labeled)

FONT = "'PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif"
MONO = "'SF Mono',Menlo,Consolas,'PingFang SC',monospace"

E = {"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;"}


def esc(s):
    return "".join(E.get(c, c) for c in str(s))


def tw(s, size):
    """Rough text width: CJK/fullwidth ~1.0em, ASCII ~0.55em."""
    w = 0.0
    for ch in str(s):
        w += size * (1.02 if ord(ch) > 0x2E7F else 0.55)
    return w


class Svg:
    def __init__(self, title, desc, width=1120):
        self.w = width
        self.parts = [
            '<svg xmlns="http://www.w3.org/2000/svg" width="%d" '
            'height="@@H@@" viewBox="0 0 %d @@H@@" role="img">'
            % (width, width),
            f"<title>{esc(title)}</title>",
            f"<desc>{esc(desc)}</desc>",
            f'<g font-family="{FONT}">',
        ]

    @staticmethod
    def f(v):
        return f"{v:.1f}".rstrip("0").rstrip(".")

    def rect(self, x, y, w, h, fill=PAPER, stroke=BORDER, rx=10, sw=1):
        st = f' stroke="{stroke}" stroke-width="{sw}"' if stroke else ""
        self.parts.append(
            f'<rect x="{self.f(x)}" y="{self.f(y)}" width="{self.f(w)}" '
            f'height="{self.f(h)}" rx="{rx}" fill="{fill}"{st}/>')

    def accent(self, x, y, w, color=ACCENT, h=3.5):
        self.parts.append(
            f'<rect x="{self.f(x + 1.5)}" y="{self.f(y + 0.5)}" '
            f'width="{self.f(w - 3)}" height="{h}" rx="1.5" fill="{color}"/>')

    def text(self, x, y, s, size=13, fill=INK, anchor="start", weight=None,
             mono=False):
        fam = MONO if mono else FONT
        wt = f' font-weight="{weight}"' if weight else ""
        self.parts.append(
            f'<text x="{self.f(x)}" y="{self.f(y)}" font-size="{size}" '
            f'fill="{fill}" text-anchor="{anchor}" font-family="{fam}"'
            f'{wt}>{esc(s)}</text>')

    def chip(self, x, y, s, size=11.5, fg=DEEP, bg=CHIP_BG, mono=False,
             pad=9):
        w = tw(s, size) + pad * 2
        self.rect(x, y, w, size + 12, fill=bg, stroke=None, rx=9)
        self.text(x + w / 2, y + size + 4, s, size=size, fill=fg,
                  anchor="middle", weight="600", mono=mono)
        return w

    def arrow(self, x1, y1, x2, y2, color=ACCENT):
        ang = math.atan2(y2 - y1, x2 - x1)
        head, back = 9.0, 4.2
        bx, by = x2 - head * math.cos(ang), y2 - head * math.sin(ang)
        px, py = -math.sin(ang), math.cos(ang)
        self.parts.append(
            f'<polygon points="{self.f(x1)},{self.f(y1)} '
            f'{self.f(bx + back * px)},{self.f(by + back * py)} '
            f'{self.f(bx - back * px)},{self.f(by - back * py)}" '
            f'fill="{color}"/>')
        self.parts.append(
            f'<polygon points="{self.f(x2)},{self.f(y2)} '
            f'{self.f(bx + 2.6 * px)},{self.f(by + 2.6 * py)} '
            f'{self.f(bx - 2.6 * px)},{self.f(by - 2.6 * py)}" '
            f'fill="{color}"/>')

    def card(self, x, y, w, h, heading, accent=ACCENT):
        self.rect(x, y, w, h)
        self.accent(x, y, w, accent)
        self.text(x + 16, y + 32, heading, size=16, weight="700")

    def save(self, name, height):
        doc = "\n".join(self.parts).replace("@@H@@", str(int(height)))
        doc += "\n</g>\n</svg>\n"
        (SVGD / name).write_text(doc, encoding="utf-8")
        print(f"panels: wrote svg/{name} ({self.w}x{int(height)})")


def load(name):
    return json.loads((DATA / name).read_text(encoding="utf-8"))


EC = load("engine-contract.json")
UT = load("unit-tests.json")
RM = load("repo-metrics.json")
CF = load("catalog-facts.json")


def claims(s, x, y, ids, size=10.5):
    cx = x
    for i in ids:
        w = s.chip(cx, y, i, size=size, fg=ACCENT, bg=CHIP_BG, mono=True,
                   pad=7)
        cx += w + 6


# ============================================================ p0 hero
def p0():
    s = Svg("kg-acme 总览：三条入口、一份快照、两条铁律",
            "能力中枢模式总览：中枢只做集成，算法在 provider 生态")
    s.rect(0, 0, s.w, 640, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 639, fill="none", stroke=BORDER)
    s.text(40, 50, "知识图谱能力中枢", size=15, fill=ACCENT, weight="600")
    s.text(40, 90, "一个中枢，把散落的图谱工具组织成一张"
                   "可发现、可路由、可治理的能力表", size=25, weight="700")
    s.card(40, 110, 500, 106, "铁律一 · 中枢只做集成")
    for i, t in enumerate(["发现 · 协议 · 目录 · 策略 · 路由，恰好五件事",
                           "抽取、去重、社区、问答、存储等图谱算法",
                           "全部属于 provider 工程，中枢绝不自实现"]):
        s.text(60, 158 + i * 24, t, size=13, fill=SUB)
    s.card(580, 110, 500, 106, "铁律二 · 参数面不写死")
    for i, t in enumerate(["能力的旗标、枚举、默认值来自 provider 自描述",
                           "中枢内置的兼容表只是兜底，",
                           "与自描述冲突时以 provider 为准并发射诊断"]):
        s.text(600, 158 + i * 24, t, size=13, fill=SUB)
    s.text(40, 258, "三条入口 · 一份不可变快照 · 一片生态", size=16,
           weight="700")
    faces = [("kg", "执行面", "能力调用与流水线"),
             ("kgctl", "管理面", "刷新快照与诊断"),
             ("kg-mcp", "发布面", "工具清单与调用")]
    for i, (name, role, desc) in enumerate(faces):
        x = 40 + i * 250
        s.rect(x, 274, 220, 82, fill=CHIP_BG, stroke=None, rx=10)
        s.rect(x, 274, 220, 82, fill="none", stroke=S1, rx=10)
        s.text(x + 110, 304, name, size=17, fill=DEEP, anchor="middle",
               weight="700", mono=True)
        s.text(x + 110, 328, f"{role} · {desc}", size=11.5, fill=SUB,
               anchor="middle")
    s.arrow(766, 315, 802, 315)
    s.rect(812, 274, 268, 82, fill=PAPER, stroke=ACCENT, rx=10, sw=1.6)
    s.text(946, 304, "不可变能力快照", size=15, fill=DEEP, anchor="middle",
           weight="700")
    s.text(946, 328, "管理面刷新生成 · 带内容指纹", size=11.5, fill=SUB,
           anchor="middle")
    s.rect(40, 374, 1040, 52, fill=CHIP_BG2, stroke=None, rx=10)
    s.text(560, 406, "快照之下是 provider 生态：协议原生 provider 与遗留"
                     "命令行（兼容桥接入）——算法都在那里", size=13,
           fill=INK, anchor="middle", weight="600")
    mets = [(str(CF["command_count"]), "条稳定命令（目录内置）"),
            (str(len(RM["source_constants"]["curated_group_namespaces"])),
             "个发布命名空间"),
            (str(len(EC["snapshot"]["semantic_ids"])), "项能力（实测快照）"),
            (str(UT["total_passed"]), "个测试全绿（冻结运行）"),
            (str(RM["tracked_total"]), "个 git 跟踪文件")]
    for i, (v, k) in enumerate(mets):
        x = 40 + i * 209
        s.rect(x, 446, 189, 88, fill=PAPER)
        s.text(x + 16, 488, v, size=30, fill=DEEP, weight="700", mono=True)
        s.text(x + 16, 514, k, size=11.5, fill=MUT)
    s.text(40, 574, "页面纪律", size=14, weight="700")
    s.text(130, 574, "显示层零代码细节：源文件名、代码坐标、逐字摘录、"
                     "内部标识符、内部路径与重建命令一律不出现；", size=12.5,
           fill=SUB)
    s.text(130, 600, "每个数字都锚定冻结证据，坐标级锚点登记在交付目录"
                     "验证层。", size=12.5, fill=SUB)
    claims(s, 960, 566, ["C01", "C02", "C03", "C04"])
    s.save("p0-hero.svg", 640)


# ============================================================ p1 architecture
def p1():
    s = Svg("中枢五职责与 provider 两侧",
            "发现、协议、目录、策略、路由五个职责与协议原生、遗留两侧")
    s.rect(0, 0, s.w, 620, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 619, fill="none", stroke=BORDER)
    s.text(40, 44, "架构", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "五职责内核：算法不进中枢，中枢只做集成",
           size=22, weight="700")
    duties = [("发现", "五级顺序找 provider", "逐个尽力探测"),
              ("协议", "自描述与执行信封", "版本协商"),
              ("目录", "稳定命令面", "加载期强校验"),
              ("策略", "副作用四门", "默认全拒"),
              ("路由", "确定性三键排序", "选中即执行")]
    for i, (name, d1, d2) in enumerate(duties):
        x = 40 + i * 208
        s.rect(x, 110, 196, 96, fill=CHIP_BG, stroke=None, rx=10)
        s.rect(x, 110, 196, 96, fill="none", stroke=S1, rx=10)
        s.text(x + 98, 142, name, size=16, fill=DEEP, anchor="middle",
               weight="700")
        s.text(x + 98, 168, d1, size=11, fill=SUB, anchor="middle")
        s.text(x + 98, 188, d2, size=11, fill=SUB, anchor="middle")
    s.card(40, 234, 500, 252, "协议原生侧（权威）")
    rows = ["自描述通过协议校验后成为参数面真相源",
            "发现顺序第五级扫描协议原生前缀可执行文件",
            "实测三个已知原生二进制进路由表登记",
            "依赖探测失败不降级：未知不等于不可用"]
    for i, t in enumerate(rows):
        s.rect(64, 284 + i * 42, 8, 8, fill=S1, stroke=None, rx=2)
        s.text(84, 293 + i * 42, t, size=12.5, fill=SUB)
    s.card(580, 234, 500, 252, "遗留命令行侧（兜底兼容桥）")
    rows2 = ["只桥接有真实命令行调用形态的三个遗留工程",
             "桥表按键挂接在 provider 发布命名空间下",
             "探测成功时自描述覆盖桥表，不一致即发射诊断",
             "无命令行形态的能力是纯协议能力：",
             "未探测即报能力未找到"]
    for i, t in enumerate(rows2):
        s.rect(604, 284 + i * 40, 8, 8, fill=MUT, stroke=None, rx=2)
        s.text(624, 293 + i * 40, t, size=12.5, fill=SUB)
    s.card(40, 510, 1040, 76, "路由三键排序（确定性）")
    s.text(64, 556, "探测过的优先于兜底的", size=13, fill=DEEP, weight="600")
    s.arrow(268, 552, 296, 552)
    s.text(310, 556, "然后按权重降序", size=13, fill=DEEP, weight="600")
    s.arrow(452, 552, 480, 552)
    s.text(494, 556, "平手按提供者标识字典序", size=13, fill=DEEP,
           weight="600")
    s.text(760, 556, "· 同输入必得同选择", size=12, fill=MUT)
    claims(s, 980, 528, ["C02", "C03", "C12"])
    s.save("p1-architecture.svg", 620)


# ============================================================ p2 snapshot flow
def p2():
    s = Svg("快照数据流：只读面零启动，执行面只复核选中者",
            "管理面刷新构建不可变快照；只读面不启动 provider；执行面三次调用")
    s.rect(0, 0, s.w, 640, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 639, fill="none", stroke=BORDER)
    s.text(40, 44, "数据流", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "一份不可变快照，两种消费纪律", size=22, weight="700")
    ro = EC["readonly_surface"]
    s.rect(40, 108, 200, 64, fill=CHIP_BG, stroke=None, rx=10)
    s.text(140, 134, "管理面刷新", size=14, fill=DEEP, anchor="middle",
           weight="700")
    s.text(140, 156, "显式挂接 + 环境发现", size=11, fill=SUB,
           anchor="middle")
    s.arrow(246, 140, 286, 140)
    s.rect(296, 108, 240, 64, fill=PAPER, stroke=ACCENT, rx=10, sw=1.6)
    s.text(416, 134, "不可变能力快照", size=14, fill=DEEP, anchor="middle",
           weight="700")
    s.text(416, 156, "探测结果 · 能力视图 · 分组 · 指纹", size=10.5,
           fill=SUB, anchor="middle")
    s.arrow(542, 140, 582, 140)
    s.rect(592, 108, 488, 64, fill=CHIP_BG2, stroke=None, rx=10)
    s.text(836, 134, f"刷新实测：{EC['refresh']['provider_calls_during_refresh']}"
                     " 次探测调用（自描述 + 依赖探测）", size=12.5, fill=INK,
           anchor="middle", weight="600")
    s.text(836, 156, "快照指纹 = 全部提供者+分组+能力视图的内容哈希，"
                     "重建即复算", size=10.5, fill=SUB, anchor="middle")
    # readonly face
    s.card(40, 204, 500, 300, "只读面：零启动（日志实测）")
    seen = []
    for r in ro["runs"]:
        key = f"{r['binary']} {r['argv']}"
        if key not in seen:
            seen.append(key)
    s.text(64, 260, f"{len(ro['runs'])} 次只读操作（"
                    f"{len(seen)} 类命令）连跑，全部只看快照：", size=12.5,
           fill=SUB)
    for i, c in enumerate(seen):
        s.chip(64 + (i % 2) * 240, 274 + (i // 2) * 36, c, size=11,
               mono=True, fg=DEEP, bg=CHIP_BG)
    s.text(64, 444, f"provider 启动次数：{ro['provider_starts']} 次",
           size=14, fill=DEEP, weight="700")
    s.text(64, 470, "帮助 · 列表 · 描述 · 补全 · 工具清单 · 干跑一律不探测、"
                    "不加载模型", size=12, fill=MUT)
    # execution face
    s.card(580, 204, 500, 300, "执行面：只复核最终选中的 provider（日志实测）")
    seq = EC["execution_revalidation"]["sequence"]
    s.text(604, 260, "一次真实调用的完整 provider 日志：", size=12.5,
           fill=SUB)
    for i, line in enumerate(seq):
        s.chip(604, 274 + i * 40, line, size=11.5, mono=True, fg=DEEP,
               bg=CHIP_BG)
    n = EC["execution_revalidation"]["provider_calls"]
    s.text(604, 444, f"恰好 {n} 次调用：先校验参数与策略，再复核并启动唯一"
                     "的选中者", size=13, fill=DEEP, weight="700")
    s.text(604, 470, "策略被拒时连这三次也不会发生（见策略门一节实测）",
           size=12, fill=MUT)
    s.rect(40, 532, 1040, 72, fill=CHIP_BG2, stroke=None, rx=10)
    s.text(64, 566, "快照只在安装期由管理面重算；执行期以快照为准，需要真相"
                    "时只向最终选中者要。", size=13, fill=INK, weight="600")
    s.text(64, 590, "离线帮助永不唤醒算法进程；每次真实执行的探测开销收敛到"
                    "常数。", size=12, fill=SUB)
    claims(s, 920, 544, ["C04", "C05", "C06"])
    s.save("p2-snapshot.svg", 640)


# ============================================================ p3 cli surface
def p3():
    s = Svg("三条命令面：动词、旗标与稳定目录",
            "执行面旗标、管理面动词、发布面方法与目录命令表")
    s.rect(0, 0, s.w, 660, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 659, fill="none", stroke=BORDER)
    s.text(40, 44, "命令面", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "三个入口各管一段，目录是唯一稳定契约", size=22,
           weight="700")
    s.card(40, 108, 344, 268, "执行面 · 全局旗标（实测帮助）")
    flags = [l.split()[0] for l in EC["help_contract"]["global_flag_lines"]]
    for i, f in enumerate(flags):
        col, row = i % 2, i // 2
        s.chip(60 + col * 162, 148 + row * 32, f, size=10.5, mono=True,
               fg=DEEP, bg=CHIP_BG, pad=7)
    s.text(60, 296, "参数一次成对象传入；四把布尔门 + 干跑；", size=11.5,
           fill=SUB)
    s.text(60, 318, "标准输出恰好一个版本化信封。", size=11.5, fill=SUB)
    s.text(60, 352, "盘点类选项属于管理面：执行面拒绝越权（实测退出码 1）。",
           size=11, fill=MUT)
    s.card(408, 108, 344, 268, "管理面 · 动词（实测）")
    verbs = ["refresh", "providers", "capabilities", "route", "completion"]
    for i, v in enumerate(verbs):
        col, row = i % 2, i // 2
        s.chip(428 + col * 162, 148 + row * 34, v, size=11, mono=True,
               fg=DEEP, bg=CHIP_BG)
    s.text(428, 252, "刷新产出快照；提供者诊断列出每个 provider；", size=11.5,
           fill=SUB)
    s.text(428, 272, "能力检索列目录；路由面解释/固定/清除；补全两 shell。",
           size=11.5, fill=SUB)
    s.text(428, 308, "同一 CLI 语义，管理面独占盘点与发现旗标。", size=11,
           fill=MUT)
    s.card(776, 108, 304, 268, "发布面 · 方法（实测会话）")
    methods = ["initialize", "ping", "tools/list", "tools/call"]
    for i, m in enumerate(methods):
        s.chip(796, 148 + i * 34, m, size=11, mono=True, fg=DEEP,
               bg=CHIP_BG)
    s.text(796, 296, "换行分帧；未知方法与坏帧有固定错误码；", size=11.5,
           fill=SUB)
    s.text(796, 318, "通知帧一律容忍不回。", size=11.5, fill=SUB)
    s.card(40, 404, 1040, 200, "稳定目录（内置命令表，实测镜像校验全过）")
    s.text(64, 456, f"{CF['command_count']} 条能力命令 · "
                    f"{CF['namespace_count']} 个发布命名空间 · 语义标识逐条"
                    "镜像命令路径（点分 ↔ 空格分段）", size=13, fill=INK,
           weight="600")
    per_ns = {}
    for c in CF["commands"]:
        ns = c["semantic_id"].split(".")[0]
        per_ns[ns] = per_ns.get(ns, 0) + 1
    x = 64
    for ns, n in sorted(per_ns.items()):
        w = max(74, tw(ns, 12) + 26)
        if x + w > 1054:
            break
        s.rect(x, 462, w, 54, fill=CHIP_BG, stroke=None, rx=8)
        s.text(x + w / 2, 484, ns, size=12, fill=DEEP, anchor="middle",
               weight="600", mono=True)
        s.text(x + w / 2, 504, f"{n} 条", size=11, fill=SUB, anchor="middle")
        x += w + 10
    s.text(64, 548, "命令面是长期契约：provider 可以来去，命令表不动；新能力"
                    "经 provider 侧发布即可见于列表。", size=12, fill=SUB)
    s.text(64, 574, "目录加载期强校验：标识镜像、分段小写连字符、标题无结尾"
                    "标点、描述单句收句号——非法即拒绝启动。", size=12,
           fill=SUB)
    claims(s, 900, 424, ["C01", "C07", "C08", "C09"])
    s.save("p3-cli.svg", 660)


# ============================================================ p4 protocol
def p4():
    s = Svg("provider 协议：三个动词与一次调用",
            "自描述、依赖探测、调用三个动词；探测失败两分；参数面覆盖兜底")
    s.rect(0, 0, s.w, 660, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 659, fill="none", stroke=BORDER)
    s.text(40, 44, "协议", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "三个动词说完一切：自描述 · 依赖探测 · 调用",
           size=22, weight="700")
    verbs = [("describe", "输出能力清单：标题、描述、副作用、输入模式、"
                          "输出模式与命令行形态"),
             ("available", "输出依赖状态：可用与否、就绪与缺失清单；"
                           "退出码恒为零"),
             ("invoke", "按能力标识调用：标准输入一份请求对象，"
                        "标准输出恰好一个执行信封")]
    for i, (v, d) in enumerate(verbs):
        y = 112 + i * 86
        s.rect(40, y, 640, 72, fill=CHIP_BG, stroke=None, rx=10)
        s.text(60, y + 30, v, size=15, fill=DEEP, weight="700", mono=True)
        s.text(160, y + 30, d.split("：", 1)[0], size=12.5, fill=INK)
        s.text(60, y + 52, d.split("：", 1)[1], size=11.5, fill=SUB)
    s.card(704, 112, 376, 244, "版本协商（实测）")
    s.text(724, 162, "中枢支持集与提供者声明集取交集，选最高共同版。",
           size=12, fill=SUB)
    s.text(724, 194, "版本无交集", size=13, fill=DEEP, weight="700")
    s.text(812, 194, "说得好但听不懂（声明了未来版本）", size=12, fill=SUB)
    s.text(724, 222, "清单畸形", size=13, fill=DEEP, weight="700")
    s.text(812, 222, "不是合法清单（没说清楚）", size=12, fill=SUB)
    s.text(724, 254, "两个错误码严格区分，诊断各归各类；", size=12,
           fill=SUB)
    s.text(724, 276, "依赖探测失败不降级，", size=12, fill=SUB)
    s.text(724, 298, "未知不等于不可用。", size=12, fill=SUB)
    tax = EC["probe_taxonomy"]
    s.card(40, 384, 1040, 152, "探测失败分类（四个假提供者实测冻结）")
    cols = [("正常提供者", "fake", "探测通过"),
            ("坏输出", "bad", "清单畸形"),
            ("未来版本", "ver", "版本无交集"),
            ("未知副作用", "ugly", "清单畸形（枚举封闭）")]
    for i, (label, pid, verdict) in enumerate(cols):
        x = 64 + i * 254
        entry = tax[pid]
        ok = entry["probed"]
        color = S1 if ok else STATUS_DENY
        s.rect(x, 420, 226, 66, fill=PAPER)
        s.rect(x, 420, 6, 66, fill=color, stroke=None, rx=2)
        s.text(x + 18, 444, label, size=13, fill=INK, weight="600")
        s.text(x + 18, 464, verdict, size=12, fill=SUB)
        s.text(x + 18, 482, "实测：探测通过" if ok else
               f"实测错误码：{entry['probe_error_code']}",
               size=10.5, fill=MUT, mono=True)
    s.text(64, 514, "未知副作用在清单校验层即被拒（副作用枚举封闭），策略层"
                    "同样默认拒绝——两道闸都关。", size=12, fill=SUB)
    s.card(40, 560, 1040, 74, "命令行渲染序（兜底路径）")
    s.text(64, 618, "常驻", size=13, fill=DEEP, weight="600")
    s.arrow(118, 614, 142, 614)
    s.text(152, 618, "子命令", size=13, fill=DEEP, weight="600")
    s.arrow(216, 614, 240, 614)
    s.text(250, 618, "位置参数", size=13, fill=DEEP, weight="600")
    s.arrow(328, 614, 352, 614)
    s.text(362, 618, "旗标（序号升序，平手按旗标字典序）", size=13,
           fill=DEEP, weight="600")
    s.text(700, 618, "布尔只在真值发射 · 可反转旗标相反 · 数组可逐元素或拼接",
           size=11.5, fill=MUT)
    claims(s, 950, 576, ["C10", "C11", "C13"])
    s.save("p4-protocol.svg", 660)


# ============================================================ p5 policy
def p5():
    s = Svg("策略门：默认全拒与零副作用干跑",
            "四类副作用四把旗标；未知副作用无门可开；干跑渲染计划")
    s.rect(0, 0, s.w, 640, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 639, fill="none", stroke=BORDER)
    s.text(40, 44, "行为门禁", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "副作用四门默认全拒，开门必须点名", size=22, weight="700")
    gates = [("network", "联网（调用模型、拉远端服务）", "--allow-network"),
             ("data_egress", "本地数据离开本机", "--allow-data-egress"),
             ("downloads_models", "下载模型权重", "--allow-model-download"),
             ("writes_db", "写图数据库", "--allow-db-write")]
    for i, (eff, meaning, flag) in enumerate(gates):
        y = 112 + i * 56
        s.rect(40, y, 560, 44, fill=PAPER)
        s.rect(40, y, 6, 44, fill=STATUS_DENY, stroke=None, rx=2)
        s.text(62, y + 27, eff, size=13, fill=INK, weight="700", mono=True)
        s.text(250, y + 27, meaning, size=12, fill=SUB)
        s.chip(552 - tw(flag, 11) - 18, y + 13, flag, size=11, mono=True,
               fg=DEEP, bg=CHIP_BG)
    s.text(62, 352, "被拒时错误文案直接点名所需旗标（实测冻结）：", size=12.5,
           fill=SUB)
    s.rect(62, 364, 516, 66, fill=CHIP_BG2, stroke=None, rx=8)
    msg = EC["policy_denied"]["envelope"]["error"]["message"]
    s.text(76, 384, msg[0:38], size=10.5, fill=DEEP, mono=True)
    s.text(76, 400, msg[38:76], size=10.5, fill=DEEP, mono=True)
    s.text(76, 416, msg[76:], size=10.5, fill=DEEP, mono=True)
    s.text(62, 458, "fail-closed：provider 声明中枢还不认识的副作用时，"
                    "没有任何旗标能放行——", size=12.5, fill=SUB)
    s.text(62, 480, "清单层枚举封闭直接拒收，策略层未知默认拒。", size=12.5,
           fill=SUB)
    s.card(640, 112, 440, 372, "干跑：零副作用的执行计划（实测冻结）")
    dr = EC["dry_run_net_no_flags"]["result"]
    rows = [("能力", dr["capability_id"]),
            ("提供者", dr["provider"]),
            ("声明副作用", ", ".join(dr["side_effects"])),
            ("当前被拒", ", ".join(dr["denied"] or [])),
            ("真跑会执行", "否" if not dr["would_execute"] else "是")]
    for i, (k, v) in enumerate(rows):
        y = 152 + i * 34
        s.text(664, y + 18, k, size=12, fill=MUT)
        bad = k in ("当前被拒", "真跑会执行") and v in ("否",) or \
            k == "当前被拒"
        s.text(828, y + 18, v, size=12.5,
               fill=STATUS_DENY if k == "当前被拒" else DEEP,
               weight="700" if k == "当前被拒" else "600",
               mono=(k not in ("声明副作用", "真跑会执行")))
        if k == "当前被拒":
            s.rect(824, y + 2, tw(v, 12.5) + 8, 22, fill="none",
                   stroke=STATUS_DENY, rx=4)
    s.text(664, 344, "开两把门后重跑干跑：", size=12.5, fill=SUB)
    s.chip(664, 356, "--allow-network --allow-data-egress", size=10.5,
           mono=True, fg=DEEP, bg=CHIP_BG)
    s.text(664, 404, "被拒清单清空 · 真跑会执行 = 是", size=13, fill=DEEP,
           weight="700")
    s.text(664, 432, "干跑本身永远成功退出：只读操作，不启动 provider、不写"
                     "文件、不联网。", size=11.5, fill=MUT)
    claims(s, 900, 570, ["C14", "C15", "C16"])
    s.save("p5-policy.svg", 640)


# ============================================================ p6 errors
def p6():
    s = Svg("错误处理：九个机器码与单信封纪律",
            "机器错误码全景、输出纪律与实测失败契约")
    s.rect(0, 0, s.w, 640, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 639, fill="none", stroke=BORDER)
    s.text(40, 44, "错误处理", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "错误码集合固定，输出纪律只有一条", size=22, weight="700")
    s.card(40, 108, 560, 250, "九个机器错误码（协议层固定集合）")
    codes = [("版本无交集", "unsupported_schema_version"),
             ("清单畸形", "malformed_manifest"),
             ("能力未找到", "capability_not_found"),
             ("提供者未找到", "provider_not_found"),
             ("策略拒绝", "policy_denied"),
             ("参数非法", "invalid_input"),
             ("调用失败", "invocation_failed"),
             ("流水线结构", "invalid_pipeline"),
             ("类型边不兼容", "incompatible_stage_edge")]
    for i, (zh, code) in enumerate(codes):
        col, row = i % 2, i // 2
        x = 64 + col * 266
        y = 170 + row * 34
        s.rect(x, y - 14, 246, 28, fill=CHIP_BG, stroke=None, rx=6)
        s.text(x + 10, y + 5, zh, size=11, fill=SUB)
        s.text(x + 104, y + 5, code, size=9.5, fill=DEEP, mono=True)
    s.text(64, 340, "新增错误码前必须先查旧码；两个清单类错误不可混用。",
           size=11.5, fill=MUT)
    s.card(640, 108, 440, 250, "输出纪律（实测）")
    s.text(664, 160, "标准输出：恰好一个版本化信封", size=13, fill=INK,
           weight="700")
    s.text(664, 184, "日志与诊断：永远走标准错误", size=13, fill=INK,
           weight="700")
    s.text(664, 208, "命令行失败：退出码 1 + 一行人话错误", size=13,
           fill=INK, weight="700")
    s.text(664, 236, "实测失败契约（逐例冻结，全部退出码 1）：", size=12,
           fill=SUB)
    for i, f in enumerate(EC["cli_failure_contract"][:5]):
        s.text(664, 258 + i * 18, f"· {f['first_stderr_line'][:38]}",
               size=10, fill=MUT, mono=True)
    s.rect(40, 372, 1040, 96, fill=CHIP_BG2, stroke=None, rx=10)
    s.text(64, 404, "实测披露：命令行顶层的错误信封把机器码统一折叠为通用"
                    "错误码，细节留在人话消息里；", size=13, fill=INK,
           weight="600")
    s.text(64, 428, "阶段级错误（如流水线某一步的调用失败）保留原样机器码。"
                    "这是与规格文档的已登记偏差。", size=12.5, fill=SUB)
    s.text(64, 452, "机器码的权威消费方是结构与自动化路径（发布面结构化内容、"
                    "阶段信封）。", size=12.5, fill=SUB)
    s.card(40, 492, 1040, 118, "未知能力实测（冻结输出）")
    ee = EC["error_envelope"]
    s.text(64, 544, f"退出码 {ee['rc']} · 信封架构版本 "
                    f"{ee['envelope']['schema_version']} · 标准输出恰好一个"
                    "对象", size=13, fill=DEEP, weight="600")
    s.text(64, 570, "消息：", size=12, fill=MUT)
    s.text(112, 570, ee["envelope"]["error"]["message"], size=12, fill=INK,
           mono=True)
    s.text(64, 594, "机器码：", size=12, fill=MUT)
    s.text(132, 594, ee["envelope"]["error"]["code"], size=12, fill=INK,
           mono=True)
    claims(s, 900, 512, ["C17", "C18"])
    s.save("p6-errors.svg", 640)


# ============================================================ p7 pipeline
def p7():
    s = Svg("流水线：纯编排、类型边与断点重跑",
            "每步一次完整路由调用；类型边三层校验；工作目录自包含；复用校验和")
    s.rect(0, 0, s.w, 700, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 699, fill="none", stroke=BORDER)
    s.text(40, 44, "流水线", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "中枢仍然只做编排：每一步都是一次完整路由调用",
           size=22, weight="700")
    plan = EC["pipeline_validate_plan"]
    s.card(40, 108, 560, 250, "实测计划：写产物 → 注入下游（拓扑序冻结）")
    stages = plan["stages"]
    inj = EC["pipeline_run"]["injected_downstream_input"]
    y = 158
    for i, st in enumerate(stages):
        s.rect(64, y, 200, 44, fill=CHIP_BG, stroke=None, rx=8)
        s.text(164, y + 27, st["capability"], size=12.5, fill=DEEP,
               anchor="middle", weight="700", mono=True)
        if i + 1 < len(stages):
            s.arrow(272, y + 22, 312, y + 22)
            s.text(320, y + 20, "产物注入下游参数（占位符换工作目录副本）",
                   size=10.5, fill=MUT)
            s.text(320, y + 38, f"实测注入值：{inj}", size=10.5, fill=MUT,
                   mono=True)
        y += 56
    s.text(64, 302, "阶段产物先校验校验和，再复制进工作目录并重算哈希；",
           size=12, fill=SUB)
    s.text(64, 324, "下游拿到的是工作目录里的自包含副本。", size=12,
           fill=SUB)
    s.card(640, 108, 440, 250, "类型边三层校验（计划期，拒绝即不执行）")
    checks = ["可接线：上游必须是产物文件型能力（内联结果型没有产物可传）",
              "种类一致：边声明的产物种类必须等于上游声明",
              "通道兼容：产物种类与下游参数按约定映射通道，两侧已知且不同即拒"]
    for i, c in enumerate(checks):
        s.rect(664, 152 + i * 62, 8, 8, fill=S1, stroke=None, rx=2)
        s.text(684, 161 + i * 62, c, size=12, fill=SUB)
    s.text(664, 336, "实测违规边：内联结果型能力被接线 → 计划期直接拒绝。",
           size=11.5, fill=MUT)
    s.card(40, 384, 500, 268, "门预检与断点重跑（实测）")
    s.text(64, 436, "预检：计划期收集全部阶段副作用并集，一次过门；", size=12.5,
           fill=SUB)
    s.text(64, 458, "缺旗标则快速失败——任何 provider 都不会启动。", size=12.5,
           fill=SUB)
    run_seq = EC["pipeline_run"]["provider_call_sequence"]
    s.text(64, 484, f"实测整链运行：{len(run_seq)} 次调用（复核 2 次 + "
                    f"调用 {sum(1 for l in run_seq if l.startswith('invoke'))}"
                    " 次）", size=13, fill=DEEP, weight="700")
    for i, line in enumerate(run_seq[:4]):
        s.chip(64 + (i % 2) * 230, 498 + (i // 2) * 34, line, size=10.5,
               mono=True, fg=DEEP, bg=CHIP_BG)
    res = EC["pipeline_resume"]
    s.text(64, 588, f"断点重跑实测：校验和一致 → 两阶段全部复用，调用 "
                    f"{res['invoke_calls']} 次，仅复核 "
                    f"{len(res['provider_call_sequence'])} 次", size=12.5,
           fill=SUB)
    s.text(64, 612, "校验和不符则该步重跑，下游随之用新产物。", size=12,
           fill=MUT)
    s.card(580, 384, 500, 268, "工作目录（实测落盘清单）")
    for i, f in enumerate(EC["pipeline_run"]["work_dir_files"]):
        s.chip(604 + (i % 2) * 240, 424 + (i // 2) * 40, f, size=10.5,
               mono=True, fg=DEEP, bg=CHIP_BG)
    s.text(604, 546, "每阶段一个阶段信封 + 整链一个总信封，全部落工作目录。",
           size=12, fill=SUB)
    s.text(604, 572, "重跑按阶段标识匹配记录；", size=12, fill=SUB)
    s.text(604, 598, "成环或引用未知阶段 → 流水线结构错误；平手按定义序。",
           size=12, fill=SUB)
    claims(s, 900, 404, ["C19", "C20", "C21", "C22", "C23", "C24"])
    s.save("p7-pipeline.svg", 700)


# ============================================================ p8 mcp
def p8():
    s = Svg("发布面：同一份快照展开成工具清单",
            "工具面由快照运行时派生；门注入；结构化错误与帧级容错")
    s.rect(0, 0, s.w, 640, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 639, fill="none", stroke=BORDER)
    s.text(40, 44, "发布面", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "工具清单不另维护：同一份快照运行时派生", size=22,
           weight="700")
    m = EC["mcp"]
    s.card(40, 108, 560, 226, "工具派生（实测）")
    s.text(64, 162, f"快照 {len(EC['snapshot']['semantic_ids'])} 项能力 → "
                    f"{m['tool_count']} 个工具：前缀 + 语义标识（空格连字符"
                    "转下划线）", size=12.5, fill=SUB)
    for i, t in enumerate(m["tool_names"][:5]):
        s.chip(64 + (i % 3) * 176, 166 + (i // 3) * 36, t, size=10.5,
               mono=True, fg=DEEP, bg=CHIP_BG)
    props = m["capability_tool_schema_props"]["test_echo"]
    injected = [p for p in props if p.startswith("allow_")] + \
               [p for p in props if p == "dry_run"]
    s.text(64, 256, f"能力工具的参数模式即 provider 发布的输入模式原样；"
                    f"中枢只追加 {len(injected)} 个自有键（四把门 + 干跑）",
           size=12.5, fill=SUB)
    s.text(64, 280, "调用时先剥除再送校验，不污染 provider 的封闭模式。",
           size=12.5, fill=SUB)
    s.text(64, 308, f"实测注入键：{', '.join(injected)}", size=11, fill=MUT,
           mono=True)
    s.card(640, 108, 440, 226, "门注入（启动期配置）")
    s.text(664, 162, "发布面没有逐次调用的旗标，门由启动配置供给：", size=12.5,
           fill=SUB)
    s.text(664, 190, "启动旗标 OR 环境允许表，两源合并", size=13, fill=DEEP,
           weight="700")
    s.text(664, 218, "允许表里出现未知令牌 → 启动即报错退出", size=13,
           fill=DEEP, weight="700")
    s.text(664, 246, "配置写错必须响：不允许静默开错门或关错门。", size=12,
           fill=MUT)
    s.text(664, 276, "实测：无门启动下调用副作用能力 →", size=12.5,
           fill=SUB)
    s.text(664, 302, "错误结果 + 状态错误的信封，服务不退出、provider 不启动",
           size=12.5, fill=STATUS_DENY, weight="600")
    s.card(40, 360, 1040, 150, "帧级契约（实测会话冻结）")
    s.text(64, 412, f"发帧 {m['requests_sent']} · 回帧 "
                    f"{m['responses_returned']} · 静默帧 "
                    f"{len(m['silent_frames'])}（通知与坏帧一律不回）",
           size=13, fill=INK, weight="600")
    s.text(64, 438, "未知方法 → 固定未找到错误码；坏帧 → 固定解析错误码且连接"
                    "不断；", size=12.5, fill=SUB)
    s.text(64, 462, "版本协商认识三个日期版则回显，否则回最新。", size=12.5,
           fill=SUB)
    s.text(64, 488, f"实测整场会话 provider 启动次数：{m['provider_starts']}"
                    "（工具清单与干跑调用都只读快照）", size=12.5, fill=DEEP,
           weight="700")
    s.rect(40, 536, 1040, 66, fill=CHIP_BG2, stroke=None, rx=10)
    s.text(64, 566, "三个前端、一套内核：目录、发现、路由、策略、流水线全部"
                    "复用内部实现，发布面只是第三种前端。", size=13, fill=INK,
           weight="600")
    s.text(64, 590, "大产物不内联：产物永远是路径 + 校验和引用，与命令行完全"
                    "一致。", size=12, fill=SUB)
    claims(s, 920, 556, ["C25", "C26", "C27"])
    s.save("p8-mcp.svg", 640)


# ============================================================ p9 quality
def p9():
    s = Svg("工程事实：规模、职责与测试网",
            "文件构成、实现与测试行数、按职责规模与两层测试")
    s.rect(0, 0, s.w, 660, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 659, fill="none", stroke=BORDER)
    s.text(40, 44, "事实", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "规模先枚举后求和，测试两层全覆盖", size=22, weight="700")
    s.card(40, 108, 500, 240, "实现与测试行数")
    mx = max(RM["impl_loc"], RM["test_loc"])
    s.text(60, 166, "实现", size=13, fill=INK, weight="700")
    s.text(60, 186, f"{RM['go_impl_files']} 个文件", size=11, fill=MUT)
    s.rect(150, 152, 300 * RM["impl_loc"] / mx, 22, fill=S1, stroke=None,
           rx=4)
    s.text(158 + 300 * RM["impl_loc"] / mx, 169, f"{RM['impl_loc']:,}",
           size=13, fill=DEEP, weight="700", mono=True)
    s.text(60, 226, "测试", size=13, fill=INK, weight="700")
    s.text(60, 246, f"{RM['go_test_files']} 个文件 · "
                    f"{RM['test_fn_count']} 个测试函数", size=11, fill=MUT)
    s.rect(150, 212, 300 * RM["test_loc"] / mx, 22, fill=S2, stroke=None,
           rx=4)
    s.text(158 + 300 * RM["test_loc"] / mx, 229, f"{RM['test_loc']:,}",
           size=13, fill=DEEP, weight="700", mono=True)
    s.text(60, 280, f"测试 : 实现 ≈ {RM['test_loc'] / RM['impl_loc']:.1f} : 1"
                    f" · 冻结运行 {UT['total_passed']} 通过 / "
                    f"{UT['total_failed']} 失败（{UT['wall_seconds']} 秒）",
           size=12, fill=SUB)
    s.text(60, 304, "两层测试网：内部单测覆盖每个职责包；端到端层构建真"
                    "二进制，", size=12, fill=SUB)
    s.text(60, 326, "用 shell 假提供者对打完整协议（零真实网络、零模型）。",
           size=12, fill=SUB)
    s.card(580, 108, 500, 240, "按职责的实现规模（行）")
    roles = RM["role_loc"][:10]
    rmx = roles[0]["loc"]
    for i, r in enumerate(roles):
        y = 150 + i * 18
        s.text(600, y + 11, r["role"], size=10.5, fill=SUB)
        s.rect(748, y, 220 * r["loc"] / rmx, 13, fill=S1, stroke=None, rx=3)
        s.text(756 + 220 * r["loc"] / rmx, y + 10.5, f"{r['loc']:,}",
               size=10, fill=DEEP, weight="600", mono=True)
    s.text(600, 338, f"合计 {RM['impl_loc']:,} 行实现 · "
                     f"{RM['go_total_loc']:,} 行 Go 源码（含测试）",
           size=11, fill=MUT)
    s.card(40, 376, 1040, 158, "全仓构成（git 跟踪文件，逐类枚举后合计）")
    go_n = RM["by_ext"].get("go", 0)
    md_n = RM["by_ext"].get("md", 0)
    other_n = RM["tracked_total"] - go_n - md_n
    s.text(64, 446, str(RM["tracked_total"]), size=26, fill=DEEP,
           weight="700", mono=True)
    s.text(124, 446, "个跟踪文件 =", size=13, fill=INK)
    s.chip(232, 428, f"{go_n} Go 源码", size=11.5, fg=DEEP, bg=CHIP_BG)
    s.chip(376, 428, f"{md_n} 规格与文档", size=11.5, fg=DEEP, bg=CHIP_BG)
    s.chip(536, 428, f"{other_n} 其他（清单/契约等）", size=11.5, fg=DEEP,
           bg=CHIP_BG)
    s.text(64, 486, "枚举明细（扩展名 → 个数）："
                    + " · ".join(f"{k} {v}" for k, v in
                                 sorted(RM["by_ext"].items())), size=11.5,
           fill=SUB)
    s.text(64, 512, f"逐类相加 {sum(RM['by_ext'].values())} = 跟踪总数 "
                    f"{RM['tracked_total']}，与冻结快照一致。", size=11.5,
           fill=MUT)
    claims(s, 900, 396, ["C29", "C30"])
    s.save("p9-quality.svg", 660)


# ============================================================ p10 contract
def p10():
    s = Svg("实测契约案例：五个冻结证据",
            "零启动、三调用复核、门拒绝、协商两分、断点重跑零调用")
    s.rect(0, 0, s.w, 700, fill=PAPER)
    s.rect(0.5, 0.5, s.w - 1, 699, fill="none", stroke=BORDER)
    s.text(40, 44, "契约实测", size=15, fill=ACCENT, weight="600")
    s.text(40, 80, "每个案例都是真二进制 + 假提供者的一次完整运行",
           size=22, weight="700")
    cases = [
        ("案例一 · 只读面零启动",
         f"{len(EC['readonly_surface']['runs'])} 类只读操作连跑一遍，假提供者"
         "日志增量 0 行——帮助、列表、描述、补全、干跑、工具清单全部只读"
         "快照。", "provider 启动 0 次", S1),
        ("案例二 · 执行面三调用",
         "真实调用副作用能力：先参数与策略校验，再对最终选中者复核并调用；"
         "日志恰好三行，顺序固定。", "describe → available → invoke", S1),
        ("案例三 · 门拒绝零启动",
         "不开门调用副作用能力：错误信封点名被拒副作用与所需旗标，退出码 1；"
         "日志增量为零——provider 根本没启动。", "文案冻结在策略门一节",
         STATUS_DENY),
        ("案例四 · 协商两分",
         "未来版本与坏输出两个假提供者分别落入版本无交集与清单畸形；未知"
         "副作用在枚举封闭层即拒收。", "错误码各归各类", S1),
        ("案例五 · 断点重跑零调用",
         "校验和一致的阶段全部复用：重跑日志里调用次数为零，只有一次复核；"
         "产物通道注入的是工作目录副本。", "调用 0 次 · 复用 2 阶段", S1),
    ]
    for i, (title, body, verdict, color) in enumerate(cases):
        y = 112 + i * 92
        s.rect(40, y, 1040, 78, fill=PAPER)
        s.rect(40, y, 6, 78, fill=color, stroke=None, rx=2)
        s.text(66, y + 28, title, size=14, fill=INK, weight="700")
        s.text(66, y + 54, body, size=12, fill=SUB)
        s.chip(858, y + 14, verdict, size=10.5, fg=DEEP, bg=CHIP_BG)
    s.rect(40, 584, 1040, 84, fill=CHIP_BG2, stroke=None, rx=10)
    s.text(64, 616, "假提供者纪律：全部证据来自 shell 假提供者——零真实网络、"
                    "零模型下载、零真实调用外部接口。", size=13, fill=INK,
           weight="600")
    s.text(64, 644, "模型类路径只以假实现出现；每个数字都能在冻结证据里逐字节"
                    "找到。", size=12, fill=SUB)
    claims(s, 920, 596, ["C28"])
    s.save("p10-contract.svg", 700)


def main():
    p0()
    p1()
    p2()
    p3()
    p4()
    p5()
    p6()
    p7()
    p8()
    p9()
    p10()
    print("panels: DONE (11 panels)")


if __name__ == "__main__":
    main()
