package sfnt

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/anaelorlinski/aaoo.textengine-go/internal/bin"
	"github.com/andybalholm/brotli"
)

// A font is untrusted input: a document can give any bytes as the source of
// an @font-face rule. Whatever the bytes, reading, subsetting and writing a
// font must return, in bounded time and memory, without a panic.

// within runs f and fails the test when it panics or does not return in time.
func within(t *testing.T, name string, limit time.Duration, f func()) {
	t.Helper()
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		f()
	}()
	select {
	case r := <-done:
		if r != nil {
			t.Errorf("%s: panic: %v", name, r)
		}
	case <-time.After(limit):
		t.Fatalf("%s: still running after %v", name, limit)
	}
}

// exercise reads a font in every format and takes it through the path of
// embedding: subset, with and without subroutines, then write.
func exercise(b []byte, glyphs []uint16) {
	plain, err := ToSFNT(b)
	if err != nil {
		return
	}
	f, err := ParseSFNT(plain, 0)
	if err != nil {
		return
	}
	for _, desubroutinize := range []bool{true, false} {
		subset, err := f.Subset(append([]uint16(nil), glyphs...), SubsetOptions{Tables: KeepPDFTables, Desubroutinize: desubroutinize})
		if err != nil {
			continue
		}
		subset.Write()
	}
}

var malformedSources = []string{"DejaVuSerif.ttf", "DejaVuSerif.woff", "DejaVuSerif.woff2", "DejaVuSerif.eot", "EBGaramond12-Regular.otf"}

func randomGlyphs(rng *rand.Rand) []uint16 {
	glyphs := []uint16{0}
	for i, n := 0, 1+rng.Intn(40); i < n; i++ {
		glyphs = append(glyphs, uint16(rng.Intn(3000)))
	}
	return glyphs
}

func TestTruncatedFonts(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, name := range malformedSources {
		b, err := os.ReadFile("resources/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 500; i++ {
			n := i // every length of the headers
			if 250 <= i {
				n = rng.Intn(len(b))
			}
			within(t, fmt.Sprintf("%s cut at %d", name, n), 10*time.Second, func() { exercise(b[:n], randomGlyphs(rng)) })
		}
	}
}

func TestMutatedFonts(t *testing.T) {
	iterations := 1500
	if testing.Short() {
		iterations = 200
	}
	rng := rand.New(rand.NewSource(11))
	for _, name := range malformedSources {
		orig, err := os.ReadFile("resources/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < iterations; i++ {
			b := append([]byte(nil), orig...)
			for j, k := 0, 1+rng.Intn(4); j < k; j++ {
				// a third of the changes in the headers, where they do the most
				p := rng.Intn(len(b))
				if rng.Intn(3) == 0 {
					p = rng.Intn(600)
				}
				switch rng.Intn(6) {
				case 0:
					b[p] = byte(rng.Intn(256))
				case 1:
					b[p] = 0xFF
				case 2:
					b[p] ^= 1 << uint(rng.Intn(8))
				case 3:
					b[p] = 0
				case 4:
					if p+2 <= len(b) {
						binary.BigEndian.PutUint16(b[p:], []uint16{0, 1, 0xFFFF, 0x7FFF, 0x8000, 0xFFFE}[rng.Intn(6)])
					}
				case 5:
					if p+4 <= len(b) {
						binary.BigEndian.PutUint32(b[p:], []uint32{0, 1, 0xFFFFFFFF, 0x7FFFFFFF, 0x80000000, uint32(len(b)), uint32(len(b)) - 1}[rng.Intn(7)])
					}
				}
			}
			glyphs := randomGlyphs(rng)
			within(t, fmt.Sprintf("%s mutation %d", name, i), 10*time.Second, func() { exercise(b, glyphs) })
		}
	}
}

// A real number in a DICT ends with a 0xF nibble: one that does not must not
// be read past the end of the data, where every byte reads as zero.
func TestDICTRealNumberWithoutEnd(t *testing.T) {
	within(t, "parseDICT", 5*time.Second, func() {
		_ = parseDICT([]byte{30, 0x12}, false, func(int, []int, []float64) bool { return true })
	})
	// the same in a font: the last byte of the Private DICT starts a number
	b, err := os.ReadFile("resources/EBGaramond12-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseSFNT(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	cff := append([]byte(nil), f.Tables["CFF "]...)
	end := f.CFF.top.PrivateOffset + f.CFF.top.PrivateLength - 1
	cff[end] = 30
	f.Tables["CFF "] = cff
	font := f.Write()
	within(t, "ParseSFNT", 5*time.Second, func() { exercise(font, []uint16{0, 1, 2}) })
}

// allocated returns the bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A compressed table is not inflated past the size its directory gives.
func TestDecompressionIsBounded(t *testing.T) {
	const declared = 1 << 20
	zeros := make([]byte, 1<<20)

	var z bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&z, 9)
	for i := 0; i < 128; i++ {
		zw.Write(zeros)
	}
	zw.Close()
	woff := make([]byte, 44+20)
	copy(woff, "wOFF")
	binary.BigEndian.PutUint32(woff[4:], 0x00010000)
	binary.BigEndian.PutUint16(woff[12:], 1)
	binary.BigEndian.PutUint32(woff[16:], 12+16+declared)
	copy(woff[44:], "head")
	binary.BigEndian.PutUint32(woff[48:], 64)
	binary.BigEndian.PutUint32(woff[52:], uint32(z.Len()))
	binary.BigEndian.PutUint32(woff[56:], declared)
	woff = append(woff, z.Bytes()...)
	binary.BigEndian.PutUint32(woff[8:], uint32(len(woff)))

	var bz bytes.Buffer
	bw := brotli.NewWriterLevel(&bz, 5)
	for i := 0; i < 128; i++ {
		bw.Write(zeros)
	}
	bw.Close()
	woff2 := make([]byte, 48)
	copy(woff2, "wOF2")
	binary.BigEndian.PutUint32(woff2[4:], 0x00010000)
	binary.BigEndian.PutUint16(woff2[12:], 1)
	binary.BigEndian.PutUint32(woff2[16:], 12+16+declared)
	binary.BigEndian.PutUint32(woff2[20:], uint32(bz.Len()))
	woff2 = append(woff2, 1 /* head, not transformed */, 0xC0, 0x80, 0x00 /* 1<<20 in base 128 */)
	woff2 = append(woff2, bz.Bytes()...)
	binary.BigEndian.PutUint32(woff2[8:], uint32(len(woff2)))

	for name, font := range map[string][]byte{"WOFF": woff, "WOFF2": woff2} {
		var err error
		n := allocated(func() { _, err = ToSFNT(font) })
		if err == nil {
			t.Errorf("%s: a table of 128 MB declared as 1 MB is accepted", name)
		}
		if 32<<20 < n {
			t.Errorf("%s of %d bytes: %d MB allocated to read a table declared as 1 MB", name, len(font), n>>20)
		}
	}

	// a table said to be longer than the file
	short := make([]byte, 100)
	copy(short, "wOFF")
	binary.BigEndian.PutUint32(short[4:], 0x00010000)
	binary.BigEndian.PutUint32(short[8:], 100)
	binary.BigEndian.PutUint16(short[12:], 1)
	binary.BigEndian.PutUint32(short[16:], 12+16+200)
	copy(short[44:], "head")
	binary.BigEndian.PutUint32(short[48:], 64)
	binary.BigEndian.PutUint32(short[52:], 200)
	binary.BigEndian.PutUint32(short[56:], 200)
	within(t, "WOFF table past the end of the file", 5*time.Second, func() {
		if _, err := ToSFNT(short); err == nil {
			t.Errorf("a table longer than the file is accepted")
		}
	})
}

// withGlyphs returns DejaVu Serif with its glyphs replaced by the given ones.
func withGlyphs(t *testing.T, glyphs [][]byte) []byte {
	t.Helper()
	data, err := os.ReadFile("resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	var glyf, loca []byte
	for _, g := range glyphs {
		loca = binary.BigEndian.AppendUint32(loca, uint32(len(glyf)))
		glyf = append(glyf, g...)
		for len(glyf)%4 != 0 {
			glyf = append(glyf, 0)
		}
	}
	n := len(glyphs)
	loca = binary.BigEndian.AppendUint32(loca, uint32(len(glyf)))
	f.Tables["glyf"] = glyf
	f.Tables["loca"] = loca
	head := append([]byte(nil), f.Tables["head"]...)
	head[50], head[51] = 0, 1 // long loca
	f.Tables["head"] = head
	maxp := append([]byte(nil), f.Tables["maxp"]...)
	binary.BigEndian.PutUint16(maxp[4:], uint16(n))
	f.Tables["maxp"] = maxp
	hhea := append([]byte(nil), f.Tables["hhea"]...)
	hhea[34], hhea[35] = 0, 1
	f.Tables["hhea"] = hhea
	f.Tables["hmtx"] = make([]byte, 4+2*(n-1))
	f.Tables["cmap"] = []byte{0, 0, 0, 0}
	post := append([]byte(nil), f.Tables["post"][:32]...)
	post[0], post[1], post[2], post[3] = 0, 3, 0, 0
	f.Tables["post"] = post
	for _, tag := range []string{"GPOS", "GSUB", "GDEF", "kern", "OS/2", "gasp", "fpgm", "prep", "cvt "} {
		delete(f.Tables, tag)
	}
	return f.Write()
}

// A composite glyph is walked once per component it names, not once per way
// of reaching it: eight levels of forty copies are 40^8 ways.
func TestCompositeGlyphFanOut(t *testing.T) {
	const copies = 40
	glyphs := [][]byte{{}}
	for k := 1; k <= 8; k++ {
		g := []byte{0xFF, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0}
		for c := 0; c < copies; c++ {
			flags := uint16(0x0002) // ARGS_ARE_XY_VALUES
			if c < copies-1 {
				flags |= 0x0020 // MORE_COMPONENTS
			}
			g = binary.BigEndian.AppendUint16(g, flags)
			g = binary.BigEndian.AppendUint16(g, uint16(k+1))
			g = append(g, 0, 0)
		}
		glyphs = append(glyphs, g)
	}
	glyphs = append(glyphs, []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x31}) // one point
	font := withGlyphs(t, glyphs)

	within(t, "Subset", 5*time.Second, func() {
		f, err := ParseSFNT(font, 0)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		deps, err := f.Glyf.Dependencies(1)
		if err != nil || len(deps) != 9 {
			t.Errorf("dependencies of glyph 1: %v, %v; want the 9 glyphs once each", deps, err)
		}
		// the subset holds the nine glyphs; drawing the outline of glyph 1
		// would take 40^8 copies of a point and is refused
		if _, err := f.Subset([]uint16{0, 1}, SubsetOptions{Tables: KeepPDFTables}); err != nil {
			t.Errorf("subset: %v", err)
		}
		if _, err := f.Glyf.Contour(1); err == nil {
			t.Errorf("the contour of a glyph of 40^8 components is computed")
		}
	})

	// a glyph of no contour with bytes after its header
	font = withGlyphs(t, [][]byte{{}, {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}})
	within(t, "glyph without contour", 5*time.Second, func() { exercise(font, []uint16{0, 1}) })
}

// A subroutine may call another ten deep, and each as many times as it
// likes: twelve calls at each of ten levels are 12^10.
func TestSubroutineFanOut(t *testing.T) {
	b, err := os.ReadFile("resources/EBGaramond12-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseSFNT(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	const depth, calls, bias = 10, 12, 107
	global := &cffINDEX{}
	for i := 0; i < depth; i++ {
		var s []byte
		for k := 0; k < calls; k++ {
			s = append(s, byte(i+1-bias+139), 29) // callgsubr i+1
		}
		global.Add(append(s, 11)) // return
	}
	global.Add([]byte{0x8b, 0x8b, 21, 11}) // 0 0 rmoveto return
	charStrings := &cffINDEX{}
	charStrings.Add([]byte{14})                           // endchar
	charStrings.Add([]byte{byte(0 - bias + 139), 29, 14}) // callgsubr 0, endchar
	cff := *f.CFF
	cff.globalSubrs, cff.charStrings, cff.charset = global, charStrings, nil
	table, err := cff.Write()
	if err != nil {
		t.Fatal(err)
	}
	f.Tables["CFF "] = table
	maxp := append([]byte(nil), f.Tables["maxp"]...)
	maxp[4], maxp[5] = 0, 2
	f.Tables["maxp"] = maxp
	hhea := append([]byte(nil), f.Tables["hhea"]...)
	hhea[34], hhea[35] = 0, 2
	f.Tables["hhea"] = hhea
	f.Tables["hmtx"] = make([]byte, 8)
	f.Tables["cmap"] = cmapWrite([]rune{'A'}, map[rune]uint16{'A': 1})
	for _, tag := range []string{"GPOS", "GSUB", "GDEF", "kern", "OS/2"} {
		delete(f.Tables, tag)
	}
	font := f.Write()

	parsed, err := ParseSFNT(font, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, desubroutinize := range []bool{true, false} {
		within(t, fmt.Sprintf("Subset, Desubroutinize %v", desubroutinize), 5*time.Second, func() {
			if _, err := parsed.Subset([]uint16{0, 1}, SubsetOptions{Tables: KeepPDFTables, Desubroutinize: desubroutinize}); err == nil {
				t.Errorf("Desubroutinize %v: a glyph of 12^10 subroutine calls is subset", desubroutinize)
			}
		})
	}
}

// An INDEX whose data needs offsets of three bytes is read back as written.
func TestINDEXOffsetSizes(t *testing.T) {
	for _, total := range []int{200, 65000, 65534, 65535, 70000, 1 << 17} {
		index := &cffINDEX{}
		for i := 0; i < 100; i++ {
			index.Add(bytes.Repeat([]byte{byte(i)}, total/100))
		}
		b, err := index.Write()
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseINDEX(bin.NewBinaryReaderBytes(b), false)
		if err != nil {
			t.Fatalf("%d bytes: %v", total, err)
		}
		if len(got.offset) != len(index.offset) || !bytes.Equal(got.data, index.data) {
			t.Fatalf("%d bytes: read back differently", total)
		}
		for i := range got.offset {
			if got.offset[i] != index.offset[i] {
				t.Fatalf("%d bytes, offsets of %d bytes: offset %d written %d, read %d", total, b[2], i, index.offset[i], got.offset[i])
			}
		}
	}
	// offsets that go back, or do not start at the beginning, are refused
	for _, b := range [][]byte{
		{0, 2, 1, 1, 5, 3, 'a', 'b', 'c', 'd'},
		{0, 1, 1, 2, 3, 'a', 'b'},
	} {
		if _, err := parseINDEX(bin.NewBinaryReaderBytes(b), false); err == nil {
			t.Errorf("INDEX % x is accepted", b)
		}
	}
}

func TestBitmapReaderReadsEveryBit(t *testing.T) {
	r := bin.NewBitmapReader([]byte{0xFF, 0x01})
	n := 0
	for i := 0; i < 16; i++ {
		if r.Read() {
			n++
		}
	}
	if n != 9 || r.EOF() {
		t.Errorf("%d bits set in ff 01, end reached: %v; want 9, false", n, r.EOF())
	}
	if r.Read() || !r.EOF() {
		t.Errorf("reading past the last bit must report the end")
	}
}
