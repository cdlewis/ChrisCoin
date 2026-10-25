// Package social renders deterministic offer images without a browser or network.
package social

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cdlewis/ChrisCoin/internal/ledger"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const Width, Height = 1200, 630

func RelativePath(issue int) string { return fmt.Sprintf("social/offers/offer-%d.png", issue) }

func Amount(amount int64) string {
	s := strconv.FormatInt(amount, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func Caption(event ledger.Event) string {
	return fmt.Sprintf("%s CC from %s to %s", Amount(event.Source.Command.Amount), event.Actor.Login, event.Source.Command.Recipient)
}

type painter struct {
	img        *image.RGBA
	bold, mono *opentype.Font
	err        error
}

func rgba(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }
func (p *painter) rect(x, y, w, h int, c color.Color) {
	draw.Draw(p.img, image.Rect(x, y, x+w, y+h), image.NewUniform(c), image.Point{}, draw.Src)
}
func (p *painter) outline(x, y, w, h, thickness int, c color.Color) {
	p.rect(x, y, w, thickness, c)
	p.rect(x, y+h-thickness, w, thickness, c)
	p.rect(x, y, thickness, h, c)
	p.rect(x+w-thickness, y, thickness, h, c)
}
func (p *painter) face(mono bool, size float64) font.Face {
	if p.err != nil {
		return nil
	}
	resource := p.bold
	if mono {
		resource = p.mono
	}
	face, err := opentype.NewFace(resource, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		p.err = err
		return nil
	}
	return face
}
func (p *painter) text(s string, x, y int, size float64, mono bool, c color.Color) {
	face := p.face(mono, size)
	if face == nil {
		return
	}
	defer face.Close()
	d := font.Drawer{Dst: p.img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}
func (p *painter) fit(s string, size, maxWidth float64, mono bool) float64 {
	for size > 8 {
		face := p.face(mono, size)
		if face == nil {
			return size
		}
		width := float64(font.MeasureString(face, s)) / 64
		_ = face.Close()
		if width <= maxWidth {
			return size
		}
		size -= 2
	}
	return size
}
func blend(a, b color.RGBA, t float64) color.RGBA {
	return rgba(uint8(float64(a.R)*(1-t)+float64(b.R)*t), uint8(float64(a.G)*(1-t)+float64(b.G)*t), uint8(float64(a.B)*(1-t)+float64(b.B)*t))
}
func (p *painter) gradientText(s string, x, y int, size float64, top, bottom color.RGBA) {
	face := p.face(false, size)
	if face == nil {
		return
	}
	defer face.Close()
	mask := image.NewAlpha(p.img.Bounds())
	d := font.Drawer{Dst: mask, Src: image.White, Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
	gradient := image.NewRGBA(p.img.Bounds())
	start := y - int(size)
	for row := 0; row < Height; row++ {
		t := max(0.0, min(1.0, float64(row-start)/size))
		draw.Draw(gradient, image.Rect(0, row, Width, row+1), image.NewUniform(blend(top, bottom, t)), image.Point{}, draw.Src)
	}
	draw.DrawMask(p.img, p.img.Bounds(), gradient, image.Point{}, mask, image.Point{}, draw.Over)
}

// Render uses only immutable offer fields. Acceptance, cancellation, timestamps,
// PR numbers, and commit hashes do not change the image or its repository bytes.
func Render(event ledger.Event) ([]byte, error) {
	if event.Type != "offer" || event.Source.Command.Action != "offer" || event.Source.Issue <= 0 || event.Source.Command.Amount <= 0 || event.Source.Command.Amount > ledger.MaxCoins {
		return nil, fmt.Errorf("social images require a valid offer")
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, err
	}
	mono, err := opentype.Parse(gomono.TTF)
	if err != nil {
		return nil, err
	}
	p := painter{img: image.NewRGBA(image.Rect(0, 0, Width, Height)), bold: bold, mono: mono}
	for y := 0; y < Height; y++ {
		p.rect(0, y, Width, 1, blend(rgba(6, 10, 35), rgba(30, 8, 61), float64(y)/Height))
	}
	// A fixed starfield makes cards reproducible across machines and retries.
	seed := uint32(0xC012C01)
	for i := 0; i < 190; i++ {
		seed = 1664525*seed + 1013904223
		x := int(seed % Width)
		seed = 1664525*seed + 1013904223
		y := int(seed % Height)
		c := rgba(75, 79, 127)
		if i%5 == 0 {
			c = rgba(149, 174, 225)
		}
		p.rect(x, y, 2, 2, c)
		if i%17 == 0 {
			p.rect(x-3, y+1, 8, 1, c)
			p.rect(x+1, y-3, 1, 8, c)
		}
	}
	cyan, magenta, gold := rgba(65, 239, 246), rgba(237, 87, 218), rgba(255, 216, 73)
	p.outline(25, 25, 1150, 580, 2, rgba(79, 76, 134))
	p.rect(25, 25, 210, 3, cyan)
	p.rect(965, 602, 210, 3, magenta)
	p.text("THE WORLD WIDE LEDGER", 64, 61, 13, true, rgba(166, 174, 216))
	p.text("CHRISCOIN", 67, 116, 49, false, rgba(104, 22, 135))
	p.gradientText("CHRISCOIN", 64, 112, 49, rgba(195, 255, 254), cyan)
	p.rect(899, 64, 237, 43, rgba(17, 15, 48))
	p.outline(899, 64, 237, 43, 1, rgba(118, 101, 166))
	badge := fmt.Sprintf("OFFER #%d", event.Source.Issue)
	p.text(badge, 915, 92, p.fit(badge, 17, 205, true), true, magenta)
	for x := 64; x < 1136; x++ {
		p.rect(x, 143, 1, 2, blend(cyan, magenta, float64(x-64)/1072))
	}
	p.text("CHRISCOIN TRANSFER", 66, 190, 14, true, rgba(178, 182, 220))
	amount := Amount(event.Source.Command.Amount) + " CC"
	size := p.fit(amount, 142, 1060, false)
	p.text(amount, 70, 319, size, false, rgba(113, 34, 103))
	p.gradientText(amount, 64, 313, size, rgba(255, 252, 204), gold)
	// The two recipient panels use the same layout for short new names and legacy names.
	for _, x := range []int{64, 680} {
		p.rect(x+5, 367, 456, 136, rgba(0, 0, 15))
		p.rect(x, 362, 456, 136, rgba(13, 17, 45))
		p.outline(x, 362, 456, 136, 1, rgba(83, 83, 133))
	}
	p.rect(64, 362, 456, 3, cyan)
	p.rect(680, 362, 456, 3, magenta)
	p.text("FROM", 84, 396, 13, true, cyan)
	p.text(event.Actor.Login, 84, 464, p.fit(event.Actor.Login, 47, 416, false), false, rgba(242, 246, 255))
	p.text("TO", 700, 396, 13, true, magenta)
	recipient := event.Source.Command.Recipient
	p.text(recipient, 700, 464, p.fit(recipient, 47, 416, false), false, rgba(255, 247, 252))
	// Pixel-built arrow; no font fallback is needed for the direction indicator.
	p.rect(555, 427, 77, 4, cyan)
	for i := 0; i < 20; i++ {
		p.rect(631-i, 427-i, 3, 3, cyan)
		p.rect(631-i, 429+i, 3, 3, cyan)
	}
	p.text("TRANSFER BY PULL REQUEST", 64, 561, 13, true, rgba(170, 168, 207))
	p.text("CHRISCOIN", 1027, 561, 16, true, gold)
	if p.err != nil {
		return nil, p.err
	}
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&out, p.img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func Generate(events []ledger.Event, directory string) error {
	if _, err := ledger.Replay(events); err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	for _, event := range events {
		if event.Type != "offer" {
			continue
		}
		data, err := Render(event)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(directory, filepath.Base(RelativePath(event.Source.Issue))), data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func ReadVerified(directory string, event ledger.Event) ([]byte, error) {
	path := filepath.Join(directory, filepath.Base(RelativePath(event.Source.Issue)))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("missing committed offer image %s: %w", path, err)
	}
	expected, err := Render(event)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, expected) {
		return nil, fmt.Errorf("offer image %s does not match its ledger entry; run generate-images", path)
	}
	return data, nil
}

func Verify(events []ledger.Event, directory string) error {
	for _, event := range events {
		if event.Type == "offer" {
			if _, err := ReadVerified(directory, event); err != nil {
				return err
			}
		}
	}
	return nil
}
