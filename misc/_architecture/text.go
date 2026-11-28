package main

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var (
	//go:embed fonts/Inter-Regular.ttf
	interRegular []byte
	//go:embed fonts/Inter-Bold.ttf
	interBold []byte
)

// typeface turns text into the outlines of its glyphs, so that the diagram
// looks the same whatever fonts the viewer has.
type typeface struct {
	font *sfnt.Font
	buf  sfnt.Buffer
	// ppem is the units per em, so that glyphs load in font units.
	ppem fixed.Int26_6
	// prefix starts the id of the definition of each glyph drawn.
	prefix string
	// ids holds the id of each glyph defined, or "" for one that draws
	// nothing, and defs the definitions in the order they were made.
	ids  map[sfnt.GlyphIndex]string
	defs []string
}

func parseTypeface(data []byte, prefix string) (*typeface, error) {
	f, err := sfnt.Parse(data)
	if err != nil {
		return nil, err
	}
	return &typeface{font: f, ppem: fixed.I(int(f.UnitsPerEm())), prefix: prefix, ids: map[sfnt.GlyphIndex]string{}}, nil
}

func (t *typeface) glyphs(s string) ([]sfnt.GlyphIndex, error) {
	gs := make([]sfnt.GlyphIndex, 0, len(s))
	for _, r := range s {
		g, err := t.font.GlyphIndex(&t.buf, r)
		if err != nil {
			return nil, err
		}
		if g == 0 {
			return nil, fmt.Errorf("the font has no glyph for %q", r)
		}
		gs = append(gs, g)
	}
	return gs, nil
}

// advances returns where each glyph of gs starts, in font units, and where
// the last one ends.
func (t *typeface) advances(gs []sfnt.GlyphIndex) ([]fixed.Int26_6, fixed.Int26_6, error) {
	xs := make([]fixed.Int26_6, len(gs))
	var x fixed.Int26_6
	for i, g := range gs {
		if i > 0 {
			k, err := t.font.Kern(&t.buf, gs[i-1], g, t.ppem, font.HintingNone)
			if err != nil && err != sfnt.ErrNotFound {
				return nil, 0, err
			}
			x += k
		}
		xs[i] = x
		adv, err := t.font.GlyphAdvance(&t.buf, g, t.ppem, font.HintingNone)
		if err != nil {
			return nil, 0, err
		}
		x += adv
	}
	return xs, x, nil
}

func (t *typeface) scale(size float64) float64 {
	return size / float64(t.font.UnitsPerEm())
}

// width returns how wide s is drawn size high.
func (t *typeface) width(s string, size float64) (float64, error) {
	gs, err := t.glyphs(s)
	if err != nil {
		return 0, err
	}
	_, end, err := t.advances(gs)
	if err != nil {
		return 0, err
	}
	return units(end) * t.scale(size), nil
}

// glyphPath returns the outline of g in font units, with its origin on the
// baseline.
func (t *typeface) glyphPath(g sfnt.GlyphIndex) (string, error) {
	segs, err := t.font.LoadGlyph(&t.buf, g, t.ppem, nil)
	if err != nil {
		return "", err
	}
	var d strings.Builder
	pt := func(p fixed.Point26_6) {
		d.WriteString(coord(units(p.X)))
		d.WriteByte(' ')
		d.WriteString(coord(units(p.Y)))
	}
	for i, seg := range segs {
		switch seg.Op {
		case sfnt.SegmentOpMoveTo:
			if i > 0 {
				d.WriteString("Z")
			}
			d.WriteString("M")
			pt(seg.Args[0])
		case sfnt.SegmentOpLineTo:
			d.WriteString("L")
			pt(seg.Args[0])
		case sfnt.SegmentOpQuadTo:
			d.WriteString("Q")
			pt(seg.Args[0])
			d.WriteByte(' ')
			pt(seg.Args[1])
		case sfnt.SegmentOpCubeTo:
			d.WriteString("C")
			pt(seg.Args[0])
			d.WriteByte(' ')
			pt(seg.Args[1])
			d.WriteByte(' ')
			pt(seg.Args[2])
		}
	}
	if len(segs) > 0 {
		d.WriteString("Z")
	}
	return d.String(), nil
}

// use returns the id of the definition of g, defining it the first time,
// and false for a glyph that draws nothing, such as a space.
func (t *typeface) use(g sfnt.GlyphIndex) (string, bool, error) {
	if id, ok := t.ids[g]; ok {
		return id, id != "", nil
	}
	d, err := t.glyphPath(g)
	if err != nil {
		return "", false, err
	}
	id := ""
	if d != "" {
		id = fmt.Sprintf("%s%d", t.prefix, g)
		t.defs = append(t.defs, fmt.Sprintf(`<path id="%s" d="%s"/>`, id, d))
	}
	t.ids[g] = id
	return id, id != "", nil
}

// draw returns s drawn size high, starting at x on the baseline y, as a
// group that places the definitions of its glyphs. transform, when set, is
// applied after that placing.
func (t *typeface) draw(s string, size, x, y float64, class, transform string) (string, error) {
	gs, err := t.glyphs(s)
	if err != nil {
		return "", err
	}
	xs, _, err := t.advances(gs)
	if err != nil {
		return "", err
	}
	place := fmt.Sprintf("translate(%s,%s) scale(%s)", coord(x), coord(y), strconv.FormatFloat(t.scale(size), 'f', 6, 64))
	if transform != "" {
		place = transform + " " + place
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<g class="%s" transform="%s">`, class, place)
	for i, g := range gs {
		id, ok, err := t.use(g)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		if xs[i] == 0 {
			fmt.Fprintf(&b, `<use xlink:href="#%s"/>`, id)
		} else {
			fmt.Fprintf(&b, `<use xlink:href="#%s" x="%s"/>`, id, coord(units(xs[i])))
		}
	}
	b.WriteString("</g>")
	return b.String(), nil
}

func units(v fixed.Int26_6) float64 {
	return float64(v) / 64
}

// coord writes v to two decimals, which is finer than any viewer draws.
func coord(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}
