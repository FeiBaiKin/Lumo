"""
阶段 8 · 自托管 CJK 显示字体：把 Google Fonts 的 Noto Serif SC 600（思源宋体半粗）
按 unicode-range 分包的 woff2 切片下载到主题目录，并改写成本地地址的 @font-face 样式表。

只做一次的构建期脚本，产物进仓库；运行时不依赖任何 CDN（主题必须能离线跑）。

取舍：
- 切片方案直接沿用 Google 的频率分包（它按汉字使用频率把 3 万字切成约 100 片，
  常用字集中在少数几片里），比自己按 Unicode 顺序切更省流量。
- 只保留与 GB2312 汉字集（6763 字）+ 基本标点 + 拉丁有交集的切片：标题里几乎不会出现
  GB2312 之外的字，真出现时按 font-family 回退到系统衬线，只有那一个字换字体。
"""

import hashlib
import re
import sys
import urllib.request
from pathlib import Path

FAMILY = "Noto Serif SC"
WEIGHT = "600"
CSS_URL = (
    "https://fonts.googleapis.com/css2?family=Noto+Serif+SC:wght@600&display=swap"
)
UA = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/128.0 Safari/537.36"
)
OUT_DIR = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("fonts/noto-serif-sc")


def fetch(url: str) -> bytes:
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=60) as resp:
        return resp.read()


def gb2312_hanzi() -> set[int]:
    """GB2312 汉字区（第 16–87 区）解码得到的码点集合。"""
    out: set[int] = set()
    for hi in range(0xB0, 0xF8):
        for lo in range(0xA1, 0xFF):
            try:
                ch = bytes([hi, lo]).decode("gb2312")
            except UnicodeDecodeError:
                continue
            out.add(ord(ch))
    return out


def wanted_codepoints() -> set[int]:
    cps = gb2312_hanzi()
    cps.update(range(0x20, 0x7F))  # ASCII
    cps.update(range(0x2000, 0x206F))  # 通用标点（引号、破折号、省略号）
    cps.update(range(0x3000, 0x303F))  # CJK 标点
    cps.update(range(0xFF00, 0xFF65))  # 全角标点与全角字母
    return cps


def parse_ranges(text: str) -> list[tuple[int, int]]:
    out: list[tuple[int, int]] = []
    for part in text.split(","):
        part = part.strip().upper().removeprefix("U+")
        if "-" in part:
            a, b = part.split("-")
            out.append((int(a, 16), int(b, 16)))
        else:
            v = int(part, 16)
            out.append((v, v))
    return out


def intersects(ranges: list[tuple[int, int]], cps: set[int]) -> bool:
    for a, b in ranges:
        # 大区间逐点判断太慢，先用端点粗筛
        if b - a > 4096:
            if any(a <= cp <= b for cp in cps):
                return True
            continue
        for cp in range(a, b + 1):
            if cp in cps:
                return True
    return False


def main() -> None:
    css = fetch(CSS_URL).decode("utf-8")
    blocks = re.findall(r"@font-face\s*\{(.*?)\}", css, flags=re.S)
    wanted = wanted_codepoints()
    OUT_DIR.mkdir(parents=True, exist_ok=True)

    kept: list[str] = []
    total = 0
    for block in blocks:
        src = re.search(r"url\((https://[^)]+\.woff2)\)", block)
        ur = re.search(r"unicode-range:\s*([^;]+);", block)
        if not src or not ur:
            continue
        ranges = parse_ranges(ur.group(1))
        if not intersects(ranges, wanted):
            continue
        url = src.group(1)
        # 切片地址形如 …HbsE.21.woff2；个别兜底文件没有编号，记作 base
        numbered = re.search(r"\.(\d+)\.woff2$", url)
        slice_id = (
            numbered.group(1)
            if numbered
            else f"base-{hashlib.sha1(url.encode()).hexdigest()[:8]}"
        )
        name = f"noto-serif-sc-600-{slice_id}.woff2"
        target = OUT_DIR / name
        if target.exists():
            data = target.read_bytes()  # 续跑时不重复下载
        else:
            data = fetch(url)
            target.write_bytes(data)
        total += len(data)
        kept.append(
            "@font-face {\n"
            f"  font-family: \"{FAMILY}\";\n"
            "  font-style: normal;\n"
            f"  font-weight: {WEIGHT};\n"
            "  font-display: swap;\n"
            f"  src: url(\"noto-serif-sc/{name}\") format(\"woff2\");\n"
            f"  unicode-range: {ur.group(1).strip()};\n"
            "}\n"
        )
        print(f"kept slice {slice_id}: {len(data)} bytes", flush=True)

    header = (
        "/*\n"
        " * 思源宋体（Noto Serif SC）半粗，按 unicode-range 分包自托管。\n"
        " * 由 scripts/fetch-display-font.py 生成；只含与 GB2312 汉字集 + 常用标点有交集的切片。\n"
        " * 许可证：SIL Open Font License 1.1。\n"
        " */\n\n"
    )
    (OUT_DIR.parent / "display-font.css").write_text(
        header + "\n".join(kept), encoding="utf-8", newline="\n"
    )
    print(f"{len(kept)} slices, {total / 1024 / 1024:.2f} MiB")


if __name__ == "__main__":
    main()
