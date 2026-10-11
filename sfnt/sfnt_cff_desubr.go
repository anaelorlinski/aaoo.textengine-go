package sfnt

import (
	"fmt"

	"github.com/anaelorlinski/aaoo.textengine-go/internal/bin"
)

// desubroutinize inlines every local and global subroutine call into each
// glyph's charstring and empties the subroutine INDEXes. Glyph outlines are
// unchanged. It mirrors the semantics of parseCharString and, like
// ReindexSubrs, only supports single-font (non-CID) CFF version 1.
//
// The (possibly shared) source font's INDEXes are not mutated: charstrings are
// written to a fresh INDEX and the subroutine INDEXes are replaced rather than
// cleared in place.
func (cff *cffTable) desubroutinize() error {
	if cff.version == 2 {
		return fmt.Errorf("CFF: desubroutinize not supported for CFF2")
	}
	if cff.fonts != nil && 1 < len(cff.fonts.localSubrs) {
		return fmt.Errorf("CFF: desubroutinize only supports single-font CFFs")
	}

	numGlyphs := uint16(cff.charStrings.Len())
	newCharStrings := &cffINDEX{}
	for glyphID := uint16(0); glyphID < numGlyphs; glyphID++ {
		cs, err := cff.inlineCharString(glyphID)
		if err != nil {
			return fmt.Errorf("CFF: glyph %d: %v", glyphID, err)
		}
		newCharStrings.Add(cs) // copies data
	}
	cff.charStrings = newCharStrings

	// Drop all subroutines. globalSubrs is a field on our cffTable copy, so
	// reassigning it is safe; localSubrs lives on the shared cffFontINDEX, so
	// copy it before emptying to avoid mutating the source font.
	cff.globalSubrs = &cffINDEX{}
	if cff.fonts != nil && 0 < len(cff.fonts.localSubrs) {
		fontsCopy := *cff.fonts
		fontsCopy.localSubrs = make([]*cffINDEX, len(cff.fonts.localSubrs))
		for i := range fontsCopy.localSubrs {
			fontsCopy.localSubrs[i] = &cffINDEX{}
		}
		cff.fonts = &fontsCopy
	}
	return nil
}

// inlineCharString returns a copy of the glyph's charstring with all callsubr
// and callgsubr operators replaced by the (recursively inlined) contents of the
// subroutines they reference. All other bytes are copied verbatim. The operand
// and stem-hint bookkeeping mirrors parseCharString so that hintmask/cntrmask
// mask lengths stay correct across inlined subroutine boundaries.
func (cff *cffTable) inlineCharString(glyphID uint16) ([]byte, error) {
	charString := cff.charStrings.Get(glyphID)
	if charString == nil {
		return nil, fmt.Errorf("bad glyphID %d", glyphID)
	} else if 65535 < len(charString) {
		return nil, fmt.Errorf("charstring too long")
	}

	// A frame is a charstring (the top-level glyph or a subroutine) being read.
	type frame struct {
		r   *bin.BinaryReader
		src []byte
	}
	cur := frame{r: bin.NewBinaryReaderBytes(charString), src: charString}
	callStack := []frame{}

	out := make([]byte, 0, len(charString))
	numLens := []int{}   // byte length in out of each pending operand
	numVals := []int32{} // value of each pending operand (16.16 fixed point)
	hints := 0
	beforeMoveto := true

	clear := func() {
		numLens = numLens[:0]
		numVals = numVals[:0]
	}

	steps := 0
	for {
		// See maxCharStringSteps. The inlined charstring is also held to
		// the length a charstring may have.
		if steps++; maxCharStringSteps < steps {
			return nil, fmt.Errorf("charstring runs for too long")
		} else if 65535 < len(out) {
			return nil, fmt.Errorf("charstring too long once its subroutines are inlined")
		}
		if cur.r.Len() == 0 {
			if len(callStack) == 0 {
				break
			}
			cur = callStack[len(callStack)-1]
			callStack = callStack[:len(callStack)-1]
			continue
		}

		start := cur.r.Pos()
		b0 := int32(cur.r.ReadUint8())

		if 32 <= b0 || b0 == cffShortint {
			// operand: copy its bytes verbatim and record it as pending
			v := cffReadCharStringNumber(cur.r, b0)
			nb := cur.src[start:cur.r.Pos()]
			out = append(out, nb...)
			numLens = append(numLens, len(nb))
			numVals = append(numVals, v)
			continue
		}

		op := b0
		if b0 == cffEscape {
			op = 256 + int32(cur.r.ReadUint8())
		}
		opBytes := cur.src[start:cur.r.Pos()]

		// Optional leading width operand (mirrors parseCharString). Only the
		// operand count used for stem-hint math is adjusted; the width bytes
		// themselves stay in the output.
		widthAdjust := 0
		if beforeMoveto {
			if op == cffHstem || op == cffVstem || op == cffVmoveto || op == cffEndchar || (cffHstemhm <= op && op <= cffVstemhm) {
				hasWidth := len(numVals)%2 == 1
				if op == cffHmoveto || op == cffVmoveto {
					hasWidth = !hasWidth
				}
				if hasWidth {
					widthAdjust = 1
				}
				if op == cffRmoveto || op == cffHmoveto || op == cffVmoveto {
					beforeMoveto = false
				}
			}
		}
		effLen := len(numVals) - widthAdjust

		switch op {
		case cffEndchar:
			if 4 <= effLen {
				// The deprecated form that builds an accented character
				// from two other glyphs, named by their standard codes:
				// nothing here follows it to those glyphs, and no reader
				// of this package draws it.
				return nil, fmt.Errorf("endchar with the operands of an accented character is not supported")
			}
			out = append(out, opBytes...)
			return out, nil
		case cffCallsubr, cffCallgsubr:
			if len(numVals) == 0 {
				return nil, ErrBadNumOperands
			}
			if 10 < len(callStack) {
				return nil, fmt.Errorf("too many nested subroutines")
			}
			_, subr, err := cff.getSubroutine(glyphID, op, numVals[len(numVals)-1])
			if err != nil {
				return nil, err
			}
			// Drop the subroutine-number operand (its bytes are the most recent
			// in out) and do not emit the call operator itself.
			idxLen := numLens[len(numLens)-1]
			out = out[:len(out)-idxLen]
			numLens = numLens[:len(numLens)-1]
			numVals = numVals[:len(numVals)-1]
			callStack = append(callStack, cur)
			cur = frame{r: bin.NewBinaryReaderBytes(subr), src: subr}
		case cffReturn:
			if len(callStack) == 0 {
				// return outside any subroutine; treat as end of glyph.
				return out, nil
			}
			cur = callStack[len(callStack)-1]
			callStack = callStack[:len(callStack)-1]
		case cffHstem, cffVstem, cffHstemhm, cffVstemhm:
			hints += effLen / 2
			out = append(out, opBytes...)
			clear()
		case cffHintmask, cffCntrmask:
			if 0 < effLen {
				// Implicit vstem hints: operands directly preceding a hintmask
				// or cntrmask declare vstem hints without the operator and
				// count toward the mask size, as in HarfBuzz's
				// determine_hintmask_size.
				hints += effLen / 2
			}
			out = append(out, opBytes...)
			mask := cur.r.ReadBytes(int64((hints + 7) / 8))
			out = append(out, mask...)
			clear()
		case cffRmoveto, cffHmoveto, cffVmoveto, cffRlineto, cffHlineto, cffVlineto,
			cffRrcurveto, cffHhcurveto, cffHvcurveto, cffRcurveline, cffRlinecurve,
			cffVhcurveto, cffVvcurveto, cffFlex, cffHflex, cffHflex1, cffFlex1:
			out = append(out, opBytes...)
			clear()
		default:
			// The arithmetic, storage and conditional operators compute
			// operands, which this function does not: it counts the operands
			// it copies to know how many stem hints there are, and a count
			// that is wrong reads a hint mask of the wrong length and takes
			// its bytes for operators. Refusing them, as parseCharString
			// does, leaves the charstring to the caller's fallback instead
			// of writing a glyph that is cut short. No font seen uses them.
			return nil, fmt.Errorf("unsupported operator %d", op)
		}
	}

	return out, nil
}
