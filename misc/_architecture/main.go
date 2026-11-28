// Command architecture draws the architecture diagram: the copy the READMEs
// show, which follows the reader's system setting, and the two the docs site
// shows, one for each of its themes.
//
// It outlines its text in the Helvetica of macOS, so it is run on a Mac. It
// is a module of its own, so that what it needs to read the font does not
// become a dependency of Probe. Run it from its directory:
//
//	go run .
package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type theme int

const (
	auto theme = iota
	light
	dark
)

const logoFile = "../probe.svg"

var outputs = []struct {
	path  string
	theme theme
}{
	{"../probe-architecture.svg", auto},
	{"../../docs/public/architecture.svg", light},
	{"../../docs/public/architecture-dark.svg", dark},
}

func main() {
	logo, err := os.ReadFile(logoFile)
	if err != nil {
		log.Fatal(err)
	}
	mark, err := readWordmark(string(logo))
	if err != nil {
		log.Fatal(err)
	}
	fs, err := loadFonts(fontFile)
	if err != nil {
		log.Fatal(err)
	}
	for _, o := range outputs {
		svg, err := render(o.theme, mark, fs)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(o.path, []byte(svg), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

// wordmark is the PROBE lettering of the logo, without its icon and frame.
type wordmark struct {
	paths []string
	// scaleX and scaleY are how the logo stretches the paths, which the
	// diagram keeps so that the letters have the logo's proportions.
	scaleX, scaleY         float64
	minX, minY, maxX, maxY float64
}

var (
	wordmarkGroup = regexp.MustCompile(`<g transform="matrix\(([^,]+),0,0,([^,]+),[^)]*\)">\s*((?:<path d="[^"]*"[^>]*/>\s*){5})</g>`)
	pathData      = regexp.MustCompile(`<path d="([^"]*)"`)
	number        = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
	relativeCmd   = regexp.MustCompile(`[^MLCZ0-9.,\s-]`)
)

// readWordmark finds the group of the five letters in the logo. Their paths
// use only absolute commands, so their bounds are those of their points.
func readWordmark(svg string) (wordmark, error) {
	m := wordmarkGroup.FindStringSubmatch(svg)
	if m == nil {
		return wordmark{}, errors.New("no group of five paths, the letters of PROBE, in the logo")
	}
	w := wordmark{minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
	var err error
	if w.scaleX, err = strconv.ParseFloat(m[1], 64); err != nil {
		return wordmark{}, err
	}
	if w.scaleY, err = strconv.ParseFloat(m[2], 64); err != nil {
		return wordmark{}, err
	}
	for _, p := range pathData.FindAllStringSubmatch(m[3], -1) {
		d := p[1]
		if relativeCmd.MatchString(d) {
			return wordmark{}, fmt.Errorf("a letter of the logo uses a command other than M, L, C and Z: %.40s", d)
		}
		nums := number.FindAllString(d, -1)
		for i := 0; i+1 < len(nums); i += 2 {
			x, _ := strconv.ParseFloat(nums[i], 64)
			y, _ := strconv.ParseFloat(nums[i+1], 64)
			w.minX, w.maxX = min(w.minX, x), max(w.maxX, x)
			w.minY, w.maxY = min(w.minY, y), max(w.maxY, y)
		}
		w.paths = append(w.paths, d)
	}
	return w, nil
}

// width is how wide the letters are drawn when they are height high.
func (w wordmark) width(height float64) float64 {
	return (w.maxX - w.minX) * w.scaleX * height / ((w.maxY - w.minY) * w.scaleY)
}

const (
	width  = 2400
	height = 1000
)

const lightStyle = `
    .s { fill: none; stroke: #000; stroke-width: 3; }
    .thick { stroke-width: 5; }
    .f { fill: #000; }
    .t { fill: #000; }
    .m { fill: #444; }
    .inv { fill: #fff; }
`

const darkRules = `
    .s { stroke: #fff; }
    .f { fill: #fff; }
    .t { fill: #fff; }
    .m { fill: #bbb; }
    .inv { fill: #000; }
`

func style(t theme) string {
	switch t {
	case auto:
		return lightStyle + "    @media (prefers-color-scheme: dark) {" + strings.ReplaceAll(darkRules, "\n    ", "\n        ") + "    }\n"
	case dark:
		return lightStyle + darkRules
	default:
		return lightStyle
	}
}

func num(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

type canvas struct {
	strings.Builder
	regular, bold *typeface
	// err is the first error text met, which render returns.
	err error
}

func (c *canvas) add(format string, args ...any) {
	_, _ = fmt.Fprintf(c, format, args...)
	c.WriteByte('\n')
}

func strokeClass(thick bool) string {
	if thick {
		return "s thick"
	}
	return "s"
}

func (c *canvas) rect(x, y, w, h, rx float64, thick, dashed bool) {
	dash := ""
	if dashed {
		dash = ` stroke-dasharray="10 8"`
	}
	c.add(`<rect class="%s" x="%s" y="%s" width="%s" height="%s" rx="%s"%s/>`, strokeClass(thick), num(x), num(y), num(w), num(h), num(rx), dash)
}

type textOpts struct {
	bold bool
	// start puts x at the start of the text rather than at its middle.
	start bool
	class string
	// transform is applied to the text once it is placed.
	transform string
}

func (c *canvas) face(bold bool) *typeface {
	if bold {
		return c.bold
	}
	return c.regular
}

func (c *canvas) textWidth(s string, size float64, bold bool) float64 {
	w, err := c.face(bold).width(s, size)
	if err != nil && c.err == nil {
		c.err = fmt.Errorf("%q: %w", s, err)
	}
	return w
}

// text draws s with its baseline at y, as the outlines of its glyphs.
func (c *canvas) text(x, y float64, s string, size float64, o textOpts) {
	if o.class == "" {
		o.class = "t"
	}
	if !o.start {
		x -= c.textWidth(s, size, o.bold) / 2
	}
	g, err := c.face(o.bold).draw(s, size, x, y, o.class, o.transform)
	if err != nil {
		if c.err == nil {
			c.err = fmt.Errorf("%q: %w", s, err)
		}
		return
	}
	c.add("%s", g)
}

type boxOpts struct {
	sub                 string
	size                float64
	dashed, bold, thick bool
}

func (c *canvas) box(x, y, w, h float64, label string, o boxOpts) {
	c.rect(x, y, w, h, 4, o.thick, o.dashed)
	labelOpts := textOpts{bold: o.bold}
	if o.sub == "" {
		c.text(x+w/2, y+h/2+o.size*0.35, label, o.size, labelOpts)
		return
	}
	c.text(x+w/2, y+h/2-4, label, o.size, labelOpts)
	c.text(x+w/2, y+h/2+26, o.sub, 19, textOpts{class: "t m"})
}

// frame draws a box whose top edge breaks for title.
func (c *canvas) frame(x, y, w, h float64, title string, size float64, thick bool) {
	tw := c.textWidth(title, size, true) + 40
	cx := x + w/2
	c.add(`<path class="%s" d="M%s,%s L%s,%s L%s,%s L%s,%s L%s,%s L%s,%s"/>`, strokeClass(thick),
		num(cx-tw/2), num(y), num(x), num(y), num(x), num(y+h), num(x+w), num(y+h), num(x+w), num(y), num(cx+tw/2), num(y))
	c.text(cx, y+size*0.35, title, size, textOpts{bold: true})
}

func (c *canvas) line(x1, y1, x2, y2 float64) {
	c.add(`<line class="s" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x1), num(y1), num(x2), num(y2))
}

// logo draws the wordmark height high with its left edge at x and its middle
// at cy.
func (c *canvas) logo(m wordmark, x, cy, height float64) {
	k := height / ((m.maxY - m.minY) * m.scaleY)
	sx, sy := k*m.scaleX, k*m.scaleY
	c.add(`<g class="f" transform="matrix(%s,0,0,%s,%s,%s)">`,
		num(sx), num(sy), num(x-m.minX*sx), num(cy-height/2-m.minY*sy))
	for _, d := range m.paths {
		c.add(`<path d="%s"/>`, d)
	}
	c.add(`</g>`)
}

func render(t theme, mark wordmark, fs fonts) (string, error) {
	regular, bold := newTypeface(fs.regular, "r"), newTypeface(fs.bold, "b")
	c := canvas{regular: regular, bold: bold}

	c.text(40, 60, "Hi-Level Architecture", 28, textOpts{bold: true, start: true})

	c.box(40, 330, 340, 170, "YAML", boxOpts{sub: "Declarative Workflow", size: 34, bold: true, thick: true})
	c.text(210, 482, "jobs · steps · needs", 19, textOpts{class: "t m"})
	c.box(40, 560, 340, 110, "Variables", boxOpts{sub: "--var · --vars-file · --env-file", size: 28, bold: true, thick: true})

	const px, py, pw, ph = 480.0, 130.0, 1100.0, 780.0
	const ex, ew = 590.0, 340.0
	const logoH, logoPadX, logoPadY = 48.0, 40.0, 25.0
	const logoX = ex + logoPadX
	for _, y := range []float64{415, 615} {
		c.line(380, y, px, y)
	}
	logoW := mark.width(logoH)
	logoBoxX, logoBoxW := logoX-logoPadX, logoW+2*logoPadX
	c.add(`<path class="s thick" d="M%s,%s L%s,%s L%s,%s L%s,%s L%s,%s L%s,%s"/>`,
		num(logoBoxX), num(py), num(px), num(py), num(px), num(py+ph), num(px+pw), num(py+ph), num(px+pw), num(py), num(logoBoxX+logoBoxW), num(py))
	c.rect(logoBoxX, py-logoH/2-logoPadY, logoBoxW, logoH+2*logoPadY, 4, true, false)
	c.logo(mark, logoX, py, logoH)
	c.add(`<rect class="f" x="%s" y="%s" width="56" height="%s"/>`, num(px), num(py), num(ph))
	c.text(0, 0, "Parser / Validator", 30, textOpts{bold: true, class: "t inv", transform: fmt.Sprintf("translate(%s,%s) rotate(-90)", num(px+38), num(py+ph/2))})

	c.text(ex+ew/2, 250, "Workflow Engine", 32, textOpts{bold: true})
	engine := []struct{ label, sub string }{
		{"Scheduler", "needs · DAG · fail_fast"},
		{"Executor", "retry · repeat · iteration"},
		{"Expr", "test · outputs · skipif"},
		{"Guard", "--read-only · --allow-host"},
		{"Printer / Reporter", "terminal · --report"},
		{"Action Runner", "go-plugin · process per step"},
	}
	for i, e := range engine {
		c.box(ex, 290+float64(i)*96, ew, 78, e.label, boxOpts{sub: e.sub, size: 27})
	}
	runnerY := 290 + float64(len(engine)-1)*96 + 39

	const bx, by, bw, bh = 1010.0, 230.0, 500.0, 520.0
	c.frame(bx, by, bw, bh, "Built-in Actions", 32, false)
	for i, name := range []string{"HTTP", "Shell", "SMTP", "SSH", "File", "Embedded"} {
		col, row := float64(i%2), float64(i/2)
		c.box(bx+40+col*225, by+50+row*90, 195, 66, name, boxOpts{size: 28})
	}
	c.text(bx+bw/2, by+bh-60, "served by the probe binary itself", 20, textOpts{class: "t m"})
	c.text(bx+bw/2, by+bh-30, "uses: http", 20, textOpts{class: "t m"})

	const xx, xw, xy, xh = 1680.0, 680.0, py, ph
	c.frame(xx, xy, xw, xh, "External Actions", 32, true)
	externals := []string{
		"Agent", "Android", "AWS",
		"Browser", "Container", "DB",
		"DNS", "GitHub Notes", "GraphQL",
		"gRPC", "IMAP", "iOS",
		"JMAP", "Mail Latency", "Redis",
		"Slack", "Slurm", "Web Socket",
	}
	for i, name := range externals {
		col, row := float64(i%3), float64(i/3)
		c.box(xx+40+col*205, xy+60+row*78, 185, 60, name, boxOpts{size: 24})
	}
	n := len(externals)
	c.box(xx+40+float64(n%3)*205, xy+60+float64(n/3)*78, 185, 60, "Your own", boxOpts{size: 24, dashed: true})
	c.text(xx+xw/2, xy+xh-160, "uses: github.com/<owner>/<repo>@<SHA>", 20, textOpts{class: "t m"})
	c.text(xx+xw/2, xy+xh-130, "or a local ./path", 20, textOpts{class: "t m"})
	c.text(xx+xw/2, xy+xh-85, "release binary named in action.yml,", 20, textOpts{class: "t m"})
	c.text(xx+xw/2, xy+xh-55, "SHA-256 checked on every start", 20, textOpts{class: "t m"})

	const branchX = bx + bw/2
	c.line(ex+ew, runnerY, xx, runnerY)
	c.line(branchX, runnerY, branchX, by+bh)
	c.add(`<circle class="f" cx="%s" cy="%s" r="6"/>`, num(branchX), num(runnerY))
	c.text(branchX+14, runnerY-24, "gRPC", 19, textOpts{start: true, class: "t m"})
	c.text(1630, runnerY-14, "gRPC", 19, textOpts{class: "t m"})

	if c.err != nil {
		return "", c.err
	}
	defs := strings.Join(append(regular.defs, bold.defs...), "\n")
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 %d %d" width="100%%">
<style>%s</style>
<defs>
%s
</defs>
%s</svg>
`, width, height, style(t), defs, c.String()), nil
}
