package sfnt

import (
	"fmt"
	"os"
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/internal/test"
)

// pathRecorder captures the sequence of path operations for a glyph so two
// renderings can be compared for exact equality.
type pathRecorder struct {
	ops []string
}

func (p *pathRecorder) MoveTo(x, y float64) { p.ops = append(p.ops, fmt.Sprintf("M %.3f %.3f", x, y)) }
func (p *pathRecorder) LineTo(x, y float64) { p.ops = append(p.ops, fmt.Sprintf("L %.3f %.3f", x, y)) }
func (p *pathRecorder) QuadTo(cx, cy, x, y float64) {
	p.ops = append(p.ops, fmt.Sprintf("Q %.3f %.3f %.3f %.3f", cx, cy, x, y))
}
func (p *pathRecorder) CubeTo(c1x, c1y, c2x, c2y, x, y float64) {
	p.ops = append(p.ops, fmt.Sprintf("C %.3f %.3f %.3f %.3f %.3f %.3f", c1x, c1y, c2x, c2y, x, y))
}
func (p *pathRecorder) Close() { p.ops = append(p.ops, "Z") }

func recordGlyph(cff *cffTable, glyphID uint16) ([]string, error) {
	rec := &pathRecorder{}
	if err := cff.ToPath(rec, glyphID, 0, 0, 0, 1.0, NoHinting); err != nil {
		return nil, err
	}
	return rec.ops, nil
}

func numSubrs(cff *cffTable) int {
	n := cff.globalSubrs.Len()
	if cff.fonts != nil {
		for _, ls := range cff.fonts.localSubrs {
			if ls != nil {
				n += ls.Len()
			}
		}
	}
	return n
}

// TestDesubroutinizeKeepsOutlines verifies that inlining CFF subroutines leaves
// every glyph's outline byte-for-byte identical, on a real CFF font that uses
// subroutines.
func TestDesubroutinizeKeepsOutlines(t *testing.T) {
	b, err := os.ReadFile("resources/EBGaramond12-Regular.otf")
	test.Error(t, err)
	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)
	test.That(t, sfnt.IsCFF && sfnt.CFF != nil, "expected a CFF font")

	numGlyphs := uint16(sfnt.CFF.charStrings.Len())
	test.That(t, 0 < numGlyphs, "no glyphs")

	// Confirm the font actually exercises subroutines, otherwise the test is
	// meaningless.
	test.That(t, 0 < numSubrs(sfnt.CFF), "test font has no subroutines; cannot validate desubroutinize")

	// Record outlines before inlining. A handful of glyphs may use operators
	// the interpreter doesn't support (independently of subroutines); skip
	// those since there is no reliable oracle for them.
	before := make([][]string, numGlyphs)
	skip := make([]bool, numGlyphs)
	compared := 0
	for g := uint16(0); g < numGlyphs; g++ {
		ops, err := recordGlyph(sfnt.CFF, g)
		if err != nil {
			skip[g] = true
			continue
		}
		before[g] = ops
	}

	test.Error(t, sfnt.CFF.desubroutinize())

	// After inlining there must be no subroutines left.
	test.T(t, numSubrs(sfnt.CFF), 0, "subroutines remain")

	// Record outlines after inlining and compare.
	for g := uint16(0); g < numGlyphs; g++ {
		if skip[g] {
			continue
		}
		after, err := recordGlyph(sfnt.CFF, g)
		test.Error(t, err, "glyph", g, "ToPath after desubroutinize")
		test.T(t, len(after), len(before[g]), "glyph", g, "op count")
		for i := range before[g] {
			if before[g][i] != after[i] {
				t.Fatalf("glyph %d op %d: %q -> %q", g, i, before[g][i], after[i])
			}
		}
		compared++
	}
	t.Logf("compared %d/%d glyphs (skipped %d unsupported)", compared, numGlyphs, int(numGlyphs)-compared)
}

// TestDesubroutinizeCntrmaskImplicitVstem verifies that the operands preceding
// a cntrmask operator declare implicit vstem hints and are counted toward the
// mask size, matching HarfBuzz's determine_hintmask_size (which runs for both
// hintmask and cntrmask). Undercounting reads too few mask bytes, after which
// the remaining mask bytes are read as operators — here the second mask byte
// is 0x0a (callsubr), which would corrupt the inlined charstring.
func TestDesubroutinizeCntrmaskImplicitVstem(t *testing.T) {
	const n20 = 159 // operand 20, single-byte encoded

	// 8 stem hints, then two operands implicitly declaring a 9th (vstem) hint
	// before cntrmask, so the mask is ceil(9/8) = 2 bytes.
	var cs []byte
	for i := 0; i < 16; i++ {
		cs = append(cs, n20)
	}
	cs = append(cs, byte(cffHstemhm), n20, n20, byte(cffCntrmask))
	cs = append(cs, 0xff, byte(cffCallsubr)) // 2 mask bytes
	cs = append(cs, byte(-107+139), byte(cffCallsubr), byte(cffEndchar))

	subr := []byte{n20, n20, byte(cffRmoveto), byte(cffReturn)}

	localSubrs := &cffINDEX{}
	localSubrs.Add(subr)
	charStrings := &cffINDEX{}
	charStrings.Add(cs)
	cff := &cffTable{
		version:     1,
		globalSubrs: &cffINDEX{},
		charStrings: charStrings,
		fonts: &cffFontINDEX{
			fds:        []uint8{0},
			localSubrs: []*cffINDEX{localSubrs},
		},
	}

	got, err := cff.inlineCharString(0)
	test.Error(t, err)

	want := append([]byte{}, cs[:20]...)            // through cntrmask
	want = append(want, 0xff, byte(cffCallsubr))    // both mask bytes, verbatim
	want = append(want, n20, n20, byte(cffRmoveto)) // subr inlined, return dropped
	want = append(want, byte(cffEndchar))
	test.T(t, fmt.Sprintf("%x", got), fmt.Sprintf("%x", want))
}

// TestSubsetDesubroutinize verifies the public API: subsetting with
// Desubroutinize set must produce a CFF without any subroutines, while the
// default keeps (reindexed) subroutines.
func TestSubsetDesubroutinize(t *testing.T) {
	b, err := os.ReadFile("resources/EBGaramond12-Regular.otf")
	test.Error(t, err)
	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)

	glyphIDs := make([]uint16, 100)
	for i := range glyphIDs {
		glyphIDs[i] = uint16(i)
	}

	kept, err := sfnt.Subset(glyphIDs, SubsetOptions{Tables: KeepPDFTables})
	test.Error(t, err)
	test.That(t, 0 < numSubrs(kept.CFF), "subset without Desubroutinize has no subroutines; cannot validate")

	flat, err := sfnt.Subset(glyphIDs, SubsetOptions{Tables: KeepPDFTables, Desubroutinize: true})
	test.Error(t, err)
	test.T(t, numSubrs(flat.CFF), 0, "subroutines remain after Desubroutinize")

	// Both subsets must draw identical outlines.
	for _, g := range glyphIDs {
		opsKept, err1 := recordGlyph(kept.CFF, g)
		opsFlat, err2 := recordGlyph(flat.CFF, g)
		if err1 != nil || err2 != nil {
			test.T(t, fmt.Sprint(err2), fmt.Sprint(err1), "glyph", g, "ToPath")
			continue
		}
		test.T(t, len(opsFlat), len(opsKept), "glyph", g, "op count")
		for i := range opsKept {
			if opsKept[i] != opsFlat[i] {
				t.Fatalf("glyph %d op %d: %q -> %q", g, i, opsKept[i], opsFlat[i])
			}
		}
	}
}
