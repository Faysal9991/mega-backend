package delivery

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
)

type ProcessedImage struct {
	Main, Thumbnail []byte
	Width, Height   int
}

func ProcessImage(data []byte, mime, checksum string) (ProcessedImage, error) {
	bad := func() (ProcessedImage, error) { return ProcessedImage{}, Fail("VALIDATION_FAILED") }
	if len(data) < 1 || int64(len(data)) > MaxImageBytes {
		return bad()
	}
	sum := sha256.Sum256(data)
	if base64.StdEncoding.EncodeToString(sum[:]) != checksum {
		return bad()
	}
	config, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || format != "png" && format != "jpeg" || mime != "image/"+format || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 4_000_000 {
		return bad()
	}
	img, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return bad()
	}
	encode := func(max int) ([]byte, int, int, error) {
		w, h := config.Width, config.Height
		if w > max || h > max {
			if w >= h {
				h = max * h / w
				w = max
			} else {
				w = max * w / h
				h = max
			}
			if w < 1 {
				w = 1
			}
			if h < 1 {
				h = 1
			}
		}
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		b := img.Bounds()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.SetNRGBA(x, y, sampleNRGBA(img, b.Min.X+x*config.Width/w, b.Min.Y+y*config.Height/h))
			}
		}
		var out bytes.Buffer
		e := png.Encode(&out, dst)
		return out.Bytes(), w, h, e
	}
	main, w, h, e := encode(1024)
	if e != nil || len(main) > 8<<20 {
		return bad()
	}
	thumb, _, _, e := encode(256)
	if e != nil {
		return bad()
	}
	return ProcessedImage{main, thumb, w, h}, nil
}

// Avoid per-pixel interface boxing for the primary JPEG/PNG decoder outputs.
// Uncommon color models retain the standard library conversion semantics.
func sampleNRGBA(img image.Image, x, y int) color.NRGBA {
	switch s := img.(type) {
	case *image.NRGBA:
		return s.NRGBAAt(x, y)
	case *image.YCbCr:
		c := s.YCbCrAt(x, y)
		r, g, b := color.YCbCrToRGB(c.Y, c.Cb, c.Cr)
		return color.NRGBA{R: r, G: g, B: b, A: 255}
	case *image.RGBA:
		c := s.RGBAAt(x, y)
		if c.A == 255 {
			return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255}
		}
		if c.A == 0 {
			return color.NRGBA{}
		}
	case *image.Gray:
		c := s.GrayAt(x, y)
		return color.NRGBA{R: c.Y, G: c.Y, B: c.Y, A: 255}
	}
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}
