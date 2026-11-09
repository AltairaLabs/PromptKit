package tts

import (
	"errors"
	"io"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
)

// resamplingReader resamples a PCM16 mono body from one rate to another as it
// is read, so synthesis still streams.
type resamplingReader struct {
	src     io.ReadCloser
	rs      *audio.PCM16StreamResampler
	buf     []byte // resampled bytes not yet returned
	readBuf []byte
	err     error // the source's terminal error (io.EOF at the end), once seen
}

func newResamplingReader(src io.ReadCloser, fromRate, toRate int) (io.ReadCloser, error) {
	rs, err := audio.NewPCM16StreamResampler(fromRate, toRate)
	if err != nil {
		_ = src.Close()
		return nil, err
	}
	return &resamplingReader{src: src, rs: rs, readBuf: make([]byte, chunkSize)}, nil
}

// Read returns resampled bytes, reading more of the source as needed. The
// source's error, io.EOF included, is returned once the resampled audio before
// it has been read, and on every Read after.
func (r *resamplingReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.buf) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		n, err := r.src.Read(r.readBuf)
		if n > 0 {
			r.buf = append(r.buf, r.rs.Write(r.readBuf[:n])...)
		}
		if err != nil {
			r.err = err
			if errors.Is(err, io.EOF) {
				r.buf = append(r.buf, r.rs.Flush()...)
			}
		}
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// Close closes the source body.
func (r *resamplingReader) Close() error { return r.src.Close() }
