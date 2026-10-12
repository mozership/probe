#!/usr/bin/env python3
"""Record the demo in the README and the docs: demo.svg and demo-ja.svg, in
misc for the README and in docs/public for the site.

    python3 misc/demo/record.py

It builds probe and the sign-up service in misc/demo/server, runs the commands
of the demo in a terminal of their own, and draws what they printed as an
animated SVG. Nothing in the picture is written by hand but the comment
lines above the commands: the commands are the ones that ran, and the output is theirs.

    python3 misc/demo/record.py --freeze 12

writes misc/demo/frame.svg, the picture as it stands 12 seconds in, without the
animation, to look at one moment of it.
"""

import argparse
import fcntl
import html
import os
import pty
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time
import unicodedata

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))

COLS = 92
FONT = 13
CHAR = 7.83  # the advance of one character of the monospace font, at FONT
LINE = 19
PAD = 20
BAR = 34  # the title bar of the terminal

FG = "#d8e6f3"
DIM = "#7f95a8"
BG = "#17222c"
FRAME = "#2f4355"
RED = "#ff7b72"
GREEN = "#56d364"
YELLOW = "#e3b341"
BLUE = "#79c0ff"
NOTE = "#a9c4dc"  # the comment that says what a scene is about

# What each scene is about, typed as a comment above its command, so that the
# eye stays where the command and its output are.
CAPTIONS = {
    "en": [
        "An AI agent wrote this E2E test. Find its mistakes before it runs.",
        "Fixed. The test calls the API, then queries the row the API wrote.",
        "On a system it must not change, refuse writes before they are sent.",
    ],
    "ja": [
        "AIエージェントが書いたE2Eテストの誤りを、実行前に見つける",
        "直したテストは、APIを呼び、DBに書き込まれた行を問い合わせる",
        "変更されては困る環境では、書き込みを送信前に拒否する",
    ],
}

# The draft is the workflow with the mistakes an agent makes: a key under
# `with` and a key of the step misspelled, and an output read by a wrong name.
MISTAKES = [
    ("      body:\n", "      bdoy:\n"),
    ('    test: res.code == 201 && res.body.status == "pending"\n', '    tset: res.code == 201 && res.body.status == "pending"\n'),
    ('      params: ["{{outputs.create.user_id}}"]\n    test: res.rows[0].status == "active"', '      params: ["{{outputs.create.userid}}"]\n    test: res.rows[0].status == "active"'),
]


def capture(cmd, cwd):
    """Run cmd in a pseudo-terminal COLS wide, and return what it printed."""
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(cwd)
        os.environ["TERM"] = "xterm-256color"
        os.execvp("/bin/sh", ["/bin/sh", "-c", cmd])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, COLS, 0, 0))
    data = b""
    while True:
        try:
            chunk = os.read(fd, 65536)
        except OSError:
            break
        if not chunk:
            break
        data += chunk
    os.waitpid(pid, 0)
    return data.decode("utf-8", "replace")


ESC = re.compile(r"\x1b\[([0-9;?]*)([A-Za-z])")


def screen(text):
    """Play text as a terminal would, and return its lines as lists of
    (character, colour, bold). A carriage return goes back to the start of the
    line and ESC[K erases the rest of it, which is how the spinner of a run
    leaves nothing behind."""
    lines = [[]]
    col = 0
    colour, bold = None, False
    i = 0
    while i < len(text):
        m = ESC.match(text, i)
        if m:
            args, final = m.groups()
            if final == "K":
                del lines[-1][col:]
            elif final == "m":
                codes = [int(c) for c in args.split(";") if c.isdigit()] or [0]
                j = 0
                while j < len(codes):
                    c = codes[j]
                    if c == 0:
                        colour, bold = None, False
                    elif c == 1:
                        bold = True
                    elif c == 22:
                        bold = False
                    elif c == 39:
                        colour = None
                    elif c == 38 and codes[j + 1 : j + 2] == [2]:
                        r, g, b = codes[j + 2 : j + 5]
                        colour = tone(r, g, b)
                        j += 4
                    elif c in (31, 91):
                        colour = RED
                    elif c in (32, 92):
                        colour = GREEN
                    elif c in (33, 93):
                        colour = YELLOW
                    elif c in (34, 94, 36, 96):
                        colour = BLUE
                    elif c in (37, 90):
                        colour = DIM
                    j += 1
            i = m.end()
            continue
        ch = text[i]
        i += 1
        if ch == "\r":
            col = 0
        elif ch == "\n":
            lines.append([])
            col = 0
        elif ch >= " ":
            cell = (ch, colour, bold)
            line = lines[-1]
            if col < len(line):
                line[col] = cell
            else:
                line.extend([(" ", None, False)] * (col - len(line)))
                line.append(cell)
            col += 1
    while lines and not "".join(c for c, _, _ in lines[-1]).strip():
        lines.pop()
    return lines


def tone(r, g, b):
    """The colours Probe prints are picked for a terminal; keep to the palette
    of the picture by taking each to the nearest of its colours."""
    if g > r and g > b:
        return GREEN
    if r > g and r > b:
        return YELLOW if g > 120 else RED
    if b > r:
        return BLUE
    return DIM


def wrap(line):
    """Break a line at COLS, as the terminal did."""
    return [line[i : i + COLS] for i in range(0, len(line), COLS)] or [[]]


def plain(text, colour=None, bold=False):
    """The cells of text. A character a terminal draws two columns wide, as it
    does kana and kanji, is followed by an empty cell that holds its second
    column."""
    cells = []
    for c in text:
        cells.append((c, colour, bold))
        if unicodedata.east_asian_width(c) in "WF":
            cells.append(("", colour, bold))
    return cells


def mark_check(line):
    """probe check prints without colour. Tint the word that says how bad a
    line is, as the picture has no other way to tell an error from a warning."""
    text = "".join(c for c, _, _ in line)
    for word, colour in ((": error:", RED), (": warning:", YELLOW)):
        at = text.find(word)
        if at >= 0:
            for k in range(at + 2, at + len(word) - 1):
                line[k] = (line[k][0], colour, True)
    return line


class Picture:
    def __init__(self, total):
        self.total = total
        self.items = []  # (row, cells, start, end, seconds a character or None)
        self.captions = []

    def scene(self, start, end, caption, steps):
        """steps are (kind, cells) in the order they appear: a comment and a
        command are typed a character at a time, the comment faster, and the
        output of a command comes a line at a time after it."""
        self.captions.append(caption)
        t = start + 0.4
        row = 0
        for kind, lines in steps:
            if kind in ("command", "comment"):
                cells = lines[0]
                if len(cells) > COLS:
                    sys.exit("record.py: a typed line is longer than the terminal is wide: %r" % "".join(c for c, _, _ in cells))
                pace = 0.045 if kind == "command" else 0.028
                self.items.append((row, cells, t, end, pace))
                t += 0.3 + pace * (len(cells) - 2) + (0.5 if kind == "command" else 0.7)
                # A comment is followed by an empty line, to set it off from the
                # command it is about.
                row += 1 if kind == "command" else 2
            else:
                for line in lines:
                    for part in wrap(line):
                        self.items.append((row, part, t, end, None))
                        row += 1
                    t += 0.12
                t += 0.6
        return row

    def rows(self):
        return max(row for row, *_ in self.items) + 1

    def render(self, freeze=None):
        width = round(COLS * CHAR + 2 * PAD)
        height = BAR + self.rows() * LINE + 2 * PAD
        frames = {}

        def show(start, end):
            """The class of what is seen from start to end of each round."""
            if freeze is not None:
                return None if start <= freeze < end else "off"
            a = round(start / self.total * 100, 2)
            b = round(end / self.total * 100, 2)
            name = "s%d_%d" % (round(a * 100), round(b * 100))
            frames[name] = (a, b)
            return name

        out = []

        def text(x, y, cells, cls=None, extra=""):
            """One line of the terminal. Each run of one colour is placed by its
            column, so that a glyph the font draws wider does not move what
            follows it."""
            runs = []
            for k, (ch, colour, bold) in enumerate(cells):
                if ch in ("", " "):
                    continue
                wide = unicodedata.east_asian_width(ch) in "WF"
                if runs and not wide and runs[-1][1:4] == (colour, bold, k):
                    runs[-1] = (runs[-1][0] + ch, colour, bold, k + 1, runs[-1][4], 0)
                else:
                    # A wide character stands alone, in the middle of its two
                    # columns, and nothing is joined to it: next is never k.
                    runs.append((ch, colour, bold, -1 if wide else k + 1, k, (2 * CHAR - FONT) / 2 if wide else 0))
            spans = []
            for s, colour, bold, _, at, shift in runs:
                attrs = ' x="%.1f"' % (x + at * CHAR + shift)
                if colour:
                    attrs += ' fill="%s"' % colour
                if bold:
                    attrs += ' font-weight="700"'
                spans.append("<tspan%s>%s</tspan>" % (attrs, html.escape(s)))
            cls = ' class="%s"' % cls if cls else ""
            out.append('<text y="%.1f"%s%s>%s</text>' % (y, cls, extra, "".join(spans)))

        top = 0
        out.append('<rect x="0.5" y="%.1f" width="%d" height="%d" rx="8" fill="%s" stroke="%s"/>' % (top + 0.5, width - 1, height - top - 1, BG, FRAME))
        for k, colour in enumerate(("#ff5f57", "#febc2e", "#28c840")):
            out.append('<circle cx="%d" cy="%d" r="5.5" fill="%s" opacity="0.85"/>' % (PAD + k * 18, top + BAR // 2, colour))

        base = top + BAR + PAD + FONT - 2
        for row, cells, start, end, pace in self.items:
            y = base + row * LINE
            if pace is None:
                cls = show(start, end)
                if cls != "off":
                    text(PAD, y, cells, cls)
                continue
            # The first two cells, "$ " or "# ", come at once, and the rest a
            # character at a time.
            cls = show(start, end)
            if cls != "off":
                text(PAD, y, cells[:2], cls)
            for k in range(2, len(cells)):
                cls = show(start + 0.3 + pace * (k - 2), end)
                if cls != "off" and cells[k][0] not in ("", " "):
                    text(PAD + k * CHAR, y, [cells[k]], cls)

        style = [
            "text{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,'Liberation Mono','Hiragino Sans','Noto Sans JP',monospace;font-size:%dpx;fill:%s;white-space:pre}" % (FONT, FG),
        ]
        for name, (a, b) in sorted(frames.items()):
            # Seen from a to b, and not before or after. The two stops a hair
            # apart make the change a step, not a fade.
            stops = []
            if a > 0:
                stops.append("0%%,%.2f%%{opacity:0}" % a)
            stops.append("%.2f%%,%.2f%%{opacity:1}" % (a + 0.01 if a > 0 else 0, b))
            if b < 100:
                stops.append("%.2f%%,100%%{opacity:0}" % (b + 0.01))
            style.append("@keyframes %s{%s}" % (name, "".join(stops)))
            style.append(".%s{opacity:0;animation:%s %ds linear infinite}" % (name, name, self.total))
        return (
            '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img">\n'
            "<title>%s</title>\n<style>%s</style>\n%s\n</svg>\n"
            % (width, height, width, height, html.escape(" ".join(self.captions)), "\n".join(style), "\n".join(out))
        )


def record(work):
    probe = os.path.join(work, "probe")
    server = os.path.join(work, "server")
    subprocess.run(["go", "build", "-o", probe, "./cmd/probe"], cwd=ROOT, check=True)
    subprocess.run(["go", "build", "-o", server, "./misc/demo/server"], cwd=ROOT, check=True)

    fixed = open(os.path.join(HERE, "signup.yml")).read()
    draft = fixed
    for right, wrong in MISTAKES:
        if right not in draft:
            sys.exit("record.py: signup.yml no longer has %r to get wrong" % right)
        draft = draft.replace(right, wrong, 1)
    workflow = os.path.join(work, "signup.yml")

    service = subprocess.Popen([server, "-db", os.path.join(work, "app.db")], cwd=work, stderr=subprocess.DEVNULL)
    try:
        time.sleep(1)
        if service.poll() is not None:
            # Most likely the port is taken, and the commands would then talk to
            # whatever has it and query a database that it does not write to.
            sys.exit("record.py: the sign-up service did not start; is 127.0.0.1:8080 in use?")
        env = "PATH=%s:$PATH " % work
        open(workflow, "w").write(draft)
        check = capture(env + "probe check signup.yml", work)
        open(workflow, "w").write(fixed)
        check_fixed = capture(env + "probe check signup.yml", work)
        run = capture(env + "probe signup.yml", work)
        refused = capture(env + "probe --read-only signup.yml 2>/dev/null", work)
    finally:
        service.terminate()
    return check, check_fixed, run, refused


def build(lang, outputs):
    check, check_fixed, run, refused = outputs
    captions = CAPTIONS[lang]

    def command(line):
        return ("command", [plain("$ ", DIM) + plain(line, FG, True)])

    def comment(line):
        return ("comment", [plain("# ", DIM) + plain(line, NOTE)])

    def output(text, mark=None):
        lines = screen(text)
        return ("output", [mark(line) for line in lines] if mark else lines)

    picture = Picture(total=30)
    picture.scene(0, 9.5, captions[0], [comment(captions[0]), command("probe check signup.yml"), output(check, mark_check)])
    picture.scene(
        9.5,
        20.5,
        captions[1],
        [comment(captions[1]), command("probe check signup.yml"), output(check_fixed), command("probe signup.yml"), output(run)],
    )
    picture.scene(
        20.5,
        30,
        captions[2],
        [comment(captions[2]), command("probe --read-only signup.yml 2>/dev/null"), output(refused)],
    )
    return picture


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--freeze", type=float, help="write misc/demo/frame.svg, the picture at this second, and nothing else")
    parser.add_argument("--lang", default="en", choices=sorted(CAPTIONS), help="the language of the frame --freeze writes")
    args = parser.parse_args()

    work = tempfile.mkdtemp(prefix="probe-demo-")
    try:
        outputs = record(work)
    finally:
        shutil.rmtree(work, ignore_errors=True)

    if args.freeze is not None:
        path = os.path.join(HERE, "frame.svg")
        open(path, "w").write(build(args.lang, outputs).render(freeze=args.freeze))
        print(path)
        return
    for lang, name in (("en", "demo.svg"), ("ja", "demo-ja.svg")):
        picture = build(lang, outputs).render()
        # The site is built from docs/ alone, so it has a copy of its own.
        for directory in ("misc", os.path.join("docs", "public")):
            path = os.path.join(ROOT, directory, name)
            open(path, "w").write(picture)
            print(path)


if __name__ == "__main__":
    main()
