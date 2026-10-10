package face

import (
	"errors"
	"os"

	"github.com/anaelorlinski/aaoo.textengine-go/sfnt"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
)

// Collection is the fonts a program draws with: those it is given in memory,
// under the families and the styles it chooses, and those the font map of
// go-text finds on the system. It keeps the file of each of them, so that a
// font is parsed once for layout, drawing and embedding.
//
// A Collection is not safe for concurrent use, as the font map it wraps.
type Collection struct {
	fm *fontscan.FontMap

	added      map[string]*Face     // the fonts given in memory, by key
	faces      map[*font.Face]*Face // the face of each font resolved
	registered map[*font.Font]Style // the style a font was last added with
	files      map[string][]byte    // the font files read from disk, by path
}

// NewCollection returns a collection over a font map of go-text, which
// resolves the fonts of a text: the fonts added to the collection are added
// to it.
func NewCollection(fm *fontscan.FontMap) *Collection {
	return &Collection{
		fm:         fm,
		added:      map[string]*Face{},
		faces:      map[*font.Face]*Face{},
		registered: map[*font.Font]Style{},
		files:      map[string][]byte{},
	}
}

// FontMap returns the font map of the collection.
func (c *Collection) FontMap() *fontscan.FontMap { return c.fm }

// Add makes the font held in data (a TTF, OTF, WOFF, WOFF2 or EOT file, with
// one font) available under the family and the style of d, whatever the font
// says of itself.
//
// key identifies the content: it is what fontscan.Location.File holds for
// the font. The same key may be added under several families and styles: the
// font is then parsed once, and data is not read again.
func (c *Collection) Add(key string, data []byte, d Description) (*Face, error) {
	f, known := c.added[key]
	if !known {
		if len(data) == 0 {
			return nil, errors.New("empty font content")
		}
		plain, err := sfnt.ToSFNT(data)
		if err != nil {
			return nil, err
		}
		lds, err := loaders(plain)
		if err != nil {
			return nil, err
		}
		if len(lds) != 1 {
			return nil, errors.New("font collections are not supported")
		}
		ft, err := font.NewFont(lds[0])
		if err != nil {
			return nil, err
		}
		f = newFace(font.NewFace(ft), plain, 0, lds[0])
		c.added[key] = f
		c.faces[f.ft] = f
	}
	style := d.Style.normalized()
	c.fm.AddFace(f.ft, fontscan.Location{File: key}, font.Description{Family: d.Family, Aspect: style.Aspect()})
	c.registered[f.ft.Font] = style
	return f, nil
}

// Lookup returns the font added under a key.
func (c *Collection) Lookup(key string) (*Face, bool) {
	f, ok := c.added[key]
	return f, ok
}

// Registered returns the style a font was added with, the last one when it
// was added several times, and false for a font that was not added: a font
// of the system, which is what it says it is.
func (c *Collection) Registered(ft *font.Font) (Style, bool) {
	style, ok := c.registered[ft]
	return style, ok
}

// Data returns the SFNT bytes of the file at a location of the font map: the
// content of a font added to the collection, or a file of the system, which
// is read once. It returns nil when the file cannot be read.
func (c *Collection) Data(location fontscan.Location) []byte {
	if f, ok := c.added[location.File]; ok {
		return f.data
	}
	if data, ok := c.files[location.File]; ok {
		return data
	}
	data, err := os.ReadFile(location.File)
	if err == nil {
		data, err = sfnt.ToSFNT(data)
	}
	if err != nil {
		data = nil
	}
	c.files[location.File] = data
	return data
}

// Face returns the face for a go-text face the font map resolved: the same
// parsed font, with the file it comes from. There is one Face per go-text
// face, so that it identifies a font in an output.
func (c *Collection) Face(ft *font.Face) *Face {
	if f, ok := c.faces[ft]; ok {
		return f
	}
	location := c.fm.FontLocation(ft.Font)
	f := FromGoText(ft, c.Data(location), int(location.Index))
	c.faces[ft] = f
	return f
}
