package delivery

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func picture(t testing.TB, w, h int, jpg bool) ([]byte, string, string) {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	im.Set(0, 0, color.NRGBA{R: 240, A: 255})
	var b bytes.Buffer
	mime := "image/png"
	var e error
	if jpg {
		e = jpeg.Encode(&b, im, nil)
		mime = "image/jpeg"
	} else {
		e = png.Encode(&b, im)
	}
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(b.Bytes())
	return b.Bytes(), mime, base64.StdEncoding.EncodeToString(sum[:])
}
func TestImageValidationAndBounds(t *testing.T) {
	for _, jpg := range []bool{false, true} {
		b, mime, sum := picture(t, 1600, 800, jpg)
		v, e := ProcessImage(b, mime, sum)
		if e != nil || v.Width != 1024 || v.Height != 512 {
			t.Fatal("image resize", e)
		}
		c, _, e := image.DecodeConfig(bytes.NewReader(v.Thumbnail))
		if e != nil || c.Width > 256 || c.Height > 256 {
			t.Fatal("thumbnail bounds", e)
		}
		for _, bad := range []struct {
			b    []byte
			m, s string
		}{{b, "image/gif", sum}, {b, mime, "bad"}, {b[:len(b)/2], mime, sum}, {[]byte("<svg/>"), "image/png", sum}} {
			if _, e := ProcessImage(bad.b, bad.m, bad.s); e == nil {
				t.Fatal("invalid image accepted")
			}
		}
	}
	b, m, s := picture(t, 4096, 1025, false)
	if _, e := ProcessImage(b, m, s); e == nil {
		t.Fatal("pixel bomb accepted")
	}
}
func TestEncryptedBindingAndSafeTemplates(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	cipher, e := Seal(key, "synthetic-token", "installation-a")
	if e != nil {
		t.Fatal(e)
	}
	plain, e := Open(key, cipher, "installation-a")
	if e != nil || plain != "synthetic-token" {
		t.Fatal("cipher roundtrip")
	}
	if _, e := Open(key, cipher, "installation-b"); e == nil {
		t.Fatal("binding substitution")
	}
	cipher[len(cipher)-1] ^= 1
	if _, e := Open(key, cipher, "installation-a"); e == nil {
		t.Fatal("cipher tampering")
	}
	if _, _, ok := PushTemplate("admin_announcement", "private-message"); ok {
		t.Fatal("unapproved template")
	}
	if _, _, ok := Template("balance"); ok {
		t.Fatal("private payload template")
	}
}
func TestAnimationAndUploadBoundary(t *testing.T) {
	for _, raw := range []string{"http://media.example/a.gif", "https://evil.invalid/a.gif", "https://media.example/a.gif?token=private", "https://media.example/../a.gif", "https://u:p@media.example/a.gif"} {
		if AnimationAllowed(raw, []string{"media.example"}) {
			t.Fatal(raw)
		}
	}
	if !AnimationAllowed("https://media.example/gifts/a.json", []string{"media.example"}) {
		t.Fatal("allowed reference")
	}
	_, _, sum := picture(t, 1, 1, false)
	if e := (Upload{Purpose: "avatar", ContentType: "image/png", Size: 1, SHA256: sum}).Validate(); e != nil {
		t.Fatal(e)
	}
	for _, purpose := range []string{"banner", "agency_banner", "arbitrary"} {
		if e := (Upload{Purpose: purpose, ContentType: "image/png", Size: 1, SHA256: sum}).Validate(); e == nil {
			t.Fatal("unsupported purpose")
		}
	}
}
func BenchmarkProcessImage(b *testing.B) {
	data, mime, sum := picture(b, 1024, 1024, false)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e := ProcessImage(data, mime, sum); e != nil {
			b.Fatal(e)
		}
	}
}

func TestSanitizedPNGPreservesColorAndTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	source.SetNRGBA(1, 0, color.NRGBA{R: 5, G: 10, B: 15, A: 255})
	var encoded bytes.Buffer
	if e := png.Encode(&encoded, source); e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(encoded.Bytes())
	out, e := ProcessImage(encoded.Bytes(), "image/png", base64.StdEncoding.EncodeToString(sum[:]))
	if e != nil {
		t.Fatal(e)
	}
	sanitized, e := png.Decode(bytes.NewReader(out.Main))
	if e != nil {
		t.Fatal(e)
	}
	for x := 0; x < 2; x++ {
		expected := color.NRGBAModel.Convert(source.At(x, 0)).(color.NRGBA)
		actual := color.NRGBAModel.Convert(sanitized.At(x, 0)).(color.NRGBA)
		if actual != expected {
			t.Fatal("pixel/alpha changed during sanitization")
		}
	}
}
