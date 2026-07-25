package framesample

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// makeAnimatedGIF builds an n-frame animated GIF of solid-color frames,
// each with the given per-frame delay (in hundredths of a second).
func makeAnimatedGIF(t *testing.T, n, delayHundredths int) []byte {
	t.Helper()
	const w, h = 8, 6
	pal := color.Palette{color.Black, color.White, color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}}
	g := &gif.GIF{Config: image.Config{Width: w, Height: h}}
	for i := range n {
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		ci := uint8(i % len(pal))
		for p := range img.Pix {
			img.Pix[p] = ci
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, delayHundredths)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

// makeJPEG encodes a tiny solid image as JPEG.
func makeJPEG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for x := range 8 {
		for y := range 6 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestGIFMJPEGSampler_GIF_AllFrames(t *testing.T) {
	data := makeAnimatedGIF(t, 4, 5)
	s := NewGIFMJPEGSampler()

	res, err := s.Sample(context.Background(), data, SampleOptions{MIMEType: "image/gif"})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(res.Frames) != 4 {
		t.Fatalf("frame count = %d, want 4", len(res.Frames))
	}
	// Frames must be valid PNGs in timeline order.
	var lastTS = res.Frames[0].Timestamp - 1
	for i, f := range res.Frames {
		if f.MIMEType != "image/png" {
			t.Errorf("frame %d MIME = %q, want image/png", i, f.MIMEType)
		}
		if _, err := png.Decode(bytes.NewReader(f.Data)); err != nil {
			t.Errorf("frame %d not a valid PNG: %v", i, err)
		}
		if f.Timestamp <= lastTS {
			t.Errorf("frame %d timestamp %v not increasing (prev %v)", i, f.Timestamp, lastTS)
		}
		lastTS = f.Timestamp
	}
}

func TestGIFMJPEGSampler_GIF_MaxFramesCap(t *testing.T) {
	data := makeAnimatedGIF(t, 20, 2)
	s := NewGIFMJPEGSampler()

	res, err := s.Sample(context.Background(), data, SampleOptions{MaxFrames: 5})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(res.Frames) != 5 {
		t.Fatalf("frame count = %d, want 5 (capped)", len(res.Frames))
	}
}

func TestGIFMJPEGSampler_GIF_DefaultCap(t *testing.T) {
	data := makeAnimatedGIF(t, 40, 1)
	s := NewGIFMJPEGSampler()

	res, err := s.Sample(context.Background(), data, SampleOptions{})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(res.Frames) != DefaultMaxFrames {
		t.Fatalf("frame count = %d, want DefaultMaxFrames %d", len(res.Frames), DefaultMaxFrames)
	}
}

func TestGIFMJPEGSampler_GIF_FramesPerSecond(t *testing.T) {
	// 10 frames at 100ms each → 1s total. Sampling at 2fps should yield
	// roughly 3 frames (t=0, 0.5, 1.0), far fewer than all 10.
	data := makeAnimatedGIF(t, 10, 10)
	s := NewGIFMJPEGSampler()

	res, err := s.Sample(context.Background(), data, SampleOptions{FramesPerSecond: 2})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(res.Frames) == 0 || len(res.Frames) >= 10 {
		t.Fatalf("fps sampling frame count = %d, want a downsampled subset (0 < n < 10)", len(res.Frames))
	}
}

func TestGIFMJPEGSampler_MJPEG_TwoFrames(t *testing.T) {
	red := makeJPEG(t, color.RGBA{R: 255, A: 255})
	green := makeJPEG(t, color.RGBA{G: 255, A: 255})
	stream := append(append([]byte{}, red...), green...)
	s := NewGIFMJPEGSampler()

	res, err := s.Sample(context.Background(), stream, SampleOptions{MIMEType: "video/x-motion-jpeg"})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(res.Frames) != 2 {
		t.Fatalf("frame count = %d, want 2", len(res.Frames))
	}
	for i, f := range res.Frames {
		if f.MIMEType != "image/jpeg" {
			t.Errorf("frame %d MIME = %q, want image/jpeg", i, f.MIMEType)
		}
		if _, err := jpeg.DecodeConfig(bytes.NewReader(f.Data)); err != nil {
			t.Errorf("frame %d not a valid JPEG: %v", i, err)
		}
	}
}

func TestGIFMJPEGSampler_SingleJPEG_OneFrame(t *testing.T) {
	data := makeJPEG(t, color.RGBA{B: 255, A: 255})
	s := NewGIFMJPEGSampler()

	res, err := s.Sample(context.Background(), data, SampleOptions{})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frame count = %d, want 1", len(res.Frames))
	}
}

func TestGIFMJPEGSampler_UnsupportedContainer(t *testing.T) {
	// Minimal MP4 ftyp box header.
	mp4 := []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0}
	s := NewGIFMJPEGSampler()

	_, err := s.Sample(context.Background(), mp4, SampleOptions{})
	if !errors.Is(err, ErrUnsupportedContainer) {
		t.Fatalf("err = %v, want ErrUnsupportedContainer", err)
	}
}

func TestGIFMJPEGSampler_UnsupportedByMIME(t *testing.T) {
	s := NewGIFMJPEGSampler()
	_, err := s.Sample(context.Background(), []byte("whatever bytes"), SampleOptions{MIMEType: "video/mp4"})
	if !errors.Is(err, ErrUnsupportedContainer) {
		t.Fatalf("err = %v, want ErrUnsupportedContainer", err)
	}
}

func TestGIFMJPEGSampler_EmptyPayload(t *testing.T) {
	s := NewGIFMJPEGSampler()
	_, err := s.Sample(context.Background(), nil, SampleOptions{})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
	if errors.Is(err, ErrUnsupportedContainer) {
		t.Fatal("empty payload should be a plain error, not ErrUnsupportedContainer")
	}
}
