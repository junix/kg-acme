#!/usr/bin/env python3
"""Long-page renderer for the kg-acme explainer infographic.

Drives chrome-headless-shell over the DevTools protocol (raw websocket,
stdlib only). Capture discipline — the fleet lesson is baked in:

  for each slice the page is SCROLLED with window.scrollTo(0, y), the
  scroll position is READ BACK and asserted (scrollY == y), and only then
  is the viewport screenshot taken. Viewport-only capture without the
  scroll+readback copies the page top on every slice.

Hard assertions:
  stitched bitmap == (1200 * dpr, pageCSSHeight * dpr)   (height equation)
  bottom edge is the page paper color; not all-white; content variance
  in the middle band; every slice's scrollY readback matched.

Outputs: render/full-2x.png, render/full-gray.png, render/thumb.png,
per-section crops under render/crops/.

Usage (from a /tmp flat copy):

    IG_OUT=/path/to/tree python3 render.py

Environment:
    IG_OUT          delivery tree root (REQUIRED; missing = hard fail)
    IG_CHROME_BIN   chrome-headless-shell executable; default = newest
                    under ~/Library/Caches/ms-playwright/
                    chromium_headless_shell-*/chrome-headless-shell-mac-*/
                    chrome-headless-shell (the documented discovery rule;
                    hard fail if none exists)
    IG_DPR          device pixel ratio (default 2)
    IG_SLICE        slice height in CSS px (default 800)
    IG_RENDER_PORT  DevTools port (default 18979)
"""

import base64
import glob
import io
import json
import os
import shutil
import struct
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

OUT = os.environ.get("IG_OUT") or ""
if not OUT:
    print("render: FATAL: IG_OUT not set (required, no default)",
          file=sys.stderr)
    sys.exit(1)
OUT = Path(OUT)
DPR = int(os.environ.get("IG_DPR", "2"))
SLICE = int(os.environ.get("IG_SLICE", "800"))
PORT = int(os.environ.get("IG_RENDER_PORT", "18979"))
PAPER = (244, 247, 251)


def die(msg):
    print(f"render: FATAL: {msg}", file=sys.stderr)
    sys.exit(1)


def find_chrome():
    env = os.environ.get("IG_CHROME_BIN")
    if env:
        if not Path(env).exists():
            die(f"IG_CHROME_BIN={env} does not exist")
        return env
    pattern = os.path.expanduser(
        "~/Library/Caches/ms-playwright/chromium_headless_shell-*/"
        "chrome-headless-shell-*/chrome-headless-shell")
    cands = sorted(glob.glob(pattern))
    if not cands:
        die("no chrome-headless-shell under the documented playwright "
            "cache path; set IG_CHROME_BIN explicitly")
    return cands[-1]  # newest chromium_headless_shell-<ver> sorts last


# ------------------------------------------------------------ tiny ws client
class WS:
    def __init__(self, url):
        self.sock = self._connect(url)
        self.buf = b""
        self.next_id = 1

    @staticmethod
    def _connect(url):
        import socket
        from urllib.parse import urlparse
        p = urlparse(url)
        sock = socket.create_connection((p.hostname, p.port), timeout=30)
        key = base64.b64encode(os.urandom(16)).decode()
        req = (f"GET {p.path} HTTP/1.1\r\nHost: {p.hostname}:{p.port}\r\n"
               "Upgrade: websocket\r\nConnection: Upgrade\r\n"
               f"Sec-WebSocket-Key: {key}\r\n"
               "Sec-WebSocket-Version: 13\r\n\r\n")
        sock.sendall(req.encode())
        resp = b""
        while b"\r\n\r\n" not in resp:
            resp += sock.recv(4096)
        if b" 101 " not in resp.split(b"\r\n", 1)[0]:
            die(f"websocket upgrade failed: {resp[:120]!r}")
        return sock

    def send(self, payload):
        data = payload.encode()
        mask = os.urandom(4)
        header = bytearray([0x81])
        n = len(data)
        if n < 126:
            header.append(0x80 | n)
        elif n < 65536:
            header.append(0x80 | 126)
            header += struct.pack(">H", n)
        else:
            header.append(0x80 | 127)
            header += struct.pack(">Q", n)
        header += mask
        masked = bytes(b ^ mask[i % 4] for i, b in enumerate(data))
        self.sock.sendall(bytes(header) + masked)

    def _read(self, n):
        while len(self.buf) < n:
            chunk = self.sock.recv(65536)
            if not chunk:
                die("socket closed mid-frame")
            self.buf += chunk
        out, self.buf = self.buf[:n], self.buf[n:]
        return out

    def recv_text(self):
        while True:
            b1, b2 = self._read(2)
            if b1 & 0x0F == 0x8:
                die("websocket closed by peer")
            if b1 & 0x0F in (0x9, 0xA):
                self._read(b2 & 0x7F)
                continue
            ln = b2 & 0x7F
            if ln == 126:
                ln = struct.unpack(">H", self._read(2))[0]
            elif ln == 127:
                ln = struct.unpack(">Q", self._read(8))[0]
            return self._read(ln).decode("utf-8", "replace")

    def call(self, method, **params):
        mid = self.next_id
        self.next_id += 1
        self.send(json.dumps({"id": mid, "method": method,
                              "params": params}))
        while True:
            msg = json.loads(self.recv_text())
            if msg.get("id") == mid:
                if "error" in msg:
                    die(f"CDP {method}: {msg['error']}")
                return msg.get("result", {})

    def eval(self, expression):
        r = self.call("Runtime.evaluate", expression=expression,
                      returnByValue=True)
        if "exceptionDetails" in r:
            die(f"evaluate failed: {r['exceptionDetails']}\n{expression}")
        return r["result"].get("value")


# ------------------------------------------------------------------- driver
def main():
    from PIL import Image

    page_path = OUT / "index.html"
    if not page_path.exists():
        die("index.html missing; run build.py first")
    render = OUT / "render"
    crops = render / "crops"
    if crops.exists():
        shutil.rmtree(crops)
    crops.mkdir(parents=True, exist_ok=True)

    chrome = find_chrome()
    print(f"render: chrome = {chrome}")
    profile = tempfile.mkdtemp(prefix="kgacme-ig-chrome-")
    url = page_path.resolve().as_uri() + f"?r={int(time.time() * 1000)}"

    proc = subprocess.Popen([
        chrome, f"--remote-debugging-port={PORT}",
        f"--user-data-dir={profile}", "--no-first-run",
        "--no-default-browser-check", "--disable-extensions",
        "--disable-sync", "--hide-scrollbars", "--disable-lcd-text",
        "--font-render-hinting=none", "--disable-gpu",
        "--window-size=1200,900", "about:blank"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        for _ in range(100):
            try:
                urllib.request.urlopen(
                    f"http://127.0.0.1:{PORT}/json/version", timeout=1).read()
                break
            except Exception:
                time.sleep(0.2)
        else:
            die("chrome-headless-shell devtools endpoint never came up")

        ws = None
        last_err = None
        for _ in range(20):
            try:
                tabs = json.loads(urllib.request.urlopen(
                    f"http://127.0.0.1:{PORT}/json", timeout=5).read())
                page_ws = next(t["webSocketDebuggerUrl"] for t in tabs
                               if t.get("type") == "page")
                ws = WS(page_ws)
                ws.call("Target.getTargets")
                break
            except StopIteration:
                last_err = "no page target in /json"
            except Exception as e:
                last_err = str(e)
            time.sleep(0.5)
        if ws is None:
            die(f"could not attach to a page target: {last_err}")

        ws.call("Page.enable")
        ws.call("Runtime.enable")
        ws.call("Page.navigate", url=url)
        for _ in range(200):
            if ws.eval("document.readyState") == "complete":
                break
            time.sleep(0.1)
        # anti-cache: hard reload ignoring cache, then wait again
        ws.call("Page.reload", ignoreCache=True)
        for _ in range(200):
            if ws.eval("document.readyState") == "complete":
                break
            time.sleep(0.1)
        time.sleep(0.8)  # settle fonts

        dims = json.loads(ws.eval(
            "JSON.stringify({w: document.documentElement.scrollWidth,"
            " h: document.documentElement.scrollHeight})"))
        W, H = dims["w"], dims["h"]
        print(f"render: page CSS {W}x{H}")
        if W != 1200:
            die(f"unexpected page width {W} (want 1200)")
        if H < 4000:
            die(f"implausibly short page {H}px — likely render failure")

        # ---- slice capture: scrollTo + scrollY readback, then viewport shot
        slices = []
        y = 0
        readbacks = []
        while y < H:
            h = min(SLICE, H - y)
            ws.call("Emulation.setDeviceMetricsOverride",
                    width=1200, height=h, deviceScaleFactor=DPR, mobile=False)
            time.sleep(0.12)
            ws.eval(f"window.scrollTo(0, {y})")
            got = ws.eval("Math.round(window.scrollY)")
            readbacks.append((y, got))
            if got != y:
                die(f"scroll readback mismatch at slice y={y}: scrollY={got} "
                    "(viewport-only capture would silently copy the page top)")
            shot = ws.call("Page.captureScreenshot", format="png")
            img = Image.open(io.BytesIO(base64.b64decode(shot["data"])))
            if img.size != (1200 * DPR, h * DPR):
                die(f"slice at y={y} (scrollY={got}) has size {img.size}, "
                    f"want {(1200 * DPR, h * DPR)}")
            slices.append(img)
            y += h
        ws.call("Emulation.clearDeviceMetricsOverride")
        print(f"render: {len(slices)} slices, scroll readbacks all matched "
              f"({readbacks[0][0]}..{readbacks[-1][0]})")

        full = Image.new("RGB", (1200 * DPR, H * DPR), "#FFFFFF")
        yy = 0
        for img in slices:
            full.paste(img, (0, yy))
            yy += img.size[1]
        # ASSERTION 1: bitmap height == page CSS height * dpr
        if full.size != (1200 * DPR, H * DPR):
            die(f"stitched {full.size} != {(1200 * DPR, H * DPR)}")
        if full.size[0] * full.size[1] * 3 < 100_000:
            die("implausibly small bitmap — empty snapshot is a hard fail")
        full.save(render / "full-2x.png")
        print(f"render: full-2x.png {full.size}")

        # ASSERTION 2: bottom edge paints the page paper; not all-white
        bg = full.getpixel((10, full.size[1] - 4))
        print(f"render: bottom-edge pixel {bg} (page paper {PAPER})")
        if bg != PAPER:
            die(f"bottom-edge pixel {bg} is not the page paper color")
        corners = [full.getpixel((5, 5)),
                   full.getpixel((full.size[0] - 5, 5)),
                   full.getpixel((full.size[0] - 5, full.size[1] - 5))]
        if all(c == (255, 255, 255) for c in corners):
            die("all-white snapshot — hard failure")
        # ASSERTION 3: content variance in the middle band
        probe = full.crop((60, full.size[1] // 3, full.size[0] - 60,
                           full.size[1] // 3 + 200)).convert("L")
        lo, hi = probe.getextrema()
        if hi - lo < 20:
            die("middle of the page is uniform blank — render failure")

        gray = full.convert("L")
        gray.save(render / "full-gray.png")
        thumb = full.copy()
        thumb.thumbnail((360, 100000))
        thumb.save(render / "thumb.png")
        print(f"render: full-gray.png + thumb.png {thumb.size}")

        # per-section crops (scroll back to top for layout geometry)
        ws.eval("window.scrollTo(0, 0)")
        rects = json.loads(ws.eval(
            "JSON.stringify(Array.from(document.querySelectorAll("
            "'section,header,footer,.disc')).map(function(e){var r="
            "e.getBoundingClientRect();return {id:e.id||"
            "e.className.split(' ')[0],"
            "top:Math.round(r.top+window.scrollY),"
            "bottom:Math.round(r.bottom+window.scrollY)};}))"))
        for i, r in enumerate(rects):
            top = max(0, r["top"]) * DPR
            bot = min(H, r["bottom"]) * DPR
            if bot - top < 20:
                continue
            name = (r["id"] or f"el{i}")
            name = "".join(c if c.isalnum() or c in "-_" else "-"
                           for c in name)[:40] or f"el{i}"
            crop = full.crop((0, top, full.size[0], bot))
            crop.save(crops / f"{i:02d}-{name}.png")
        n_crops = len(list(crops.glob("*.png")))
        print(f"render: {n_crops} crops -> render/crops/")
        if n_crops < len(SECTION_MIN):
            die(f"expected at least {len(SECTION_MIN)} crops, got {n_crops}")
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
        shutil.rmtree(profile, ignore_errors=True)
    print("render: DONE")


SECTION_MIN = ["s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9",
               "s10"]

if __name__ == "__main__":
    main()
