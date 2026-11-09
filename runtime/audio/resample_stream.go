package audio

import (
	"encoding/binary"
	"fmt"
)

// pcm16BytesPerSample is the size of one 16-bit mono sample.
const pcm16BytesPerSample = 2

// PCM16StreamResampler resamples a stream of little-endian 16-bit mono PCM
// that arrives in chunks of any size, odd byte counts included. It
// interpolates linearly, like ResamplePCM16, but keeps its position across
// chunks, so how the input is chunked does not change the output. Once Flush
// is called it has produced floor(inputSamples * toRate / fromRate) samples,
// as one ResamplePCM16 call over the whole stream does; it computes positions
// exactly, so a sample can differ from ResamplePCM16's by one step of
// rounding.
//
// A PCM16StreamResampler is not safe for concurrent use.
type PCM16StreamResampler struct {
	from, to int64
	carry    []byte  // a trailing odd byte, completed by the next chunk
	window   []int16 // input samples from index base on
	base     int64   // global index of window[0]
	inTotal  int64   // input samples seen
	outIndex int64   // next output sample to produce
}

// NewPCM16StreamResampler returns a resampler from fromRate to toRate. It
// returns an error for a non-positive rate.
func NewPCM16StreamResampler(fromRate, toRate int) (*PCM16StreamResampler, error) {
	if fromRate <= 0 || toRate <= 0 {
		return nil, fmt.Errorf("invalid sample rates: from=%d, to=%d", fromRate, toRate)
	}
	return &PCM16StreamResampler{from: int64(fromRate), to: int64(toRate)}, nil
}

// Write takes the next chunk of input and returns the output samples it
// completes. An output sample is completed once both input samples it
// interpolates between have arrived.
func (r *PCM16StreamResampler) Write(chunk []byte) []byte {
	data := chunk
	if len(r.carry) > 0 {
		data = make([]byte, 0, len(r.carry)+len(chunk))
		data = append(data, r.carry...)
		data = append(data, chunk...)
		r.carry = nil
	}
	if len(data)%pcm16BytesPerSample == 1 {
		r.carry = []byte{data[len(data)-1]}
		data = data[:len(data)-1]
	}
	for i := 0; i+1 < len(data); i += pcm16BytesPerSample {
		r.window = append(r.window, int16(binary.LittleEndian.Uint16(data[i:]))) //nolint:gosec // PCM16
	}
	r.inTotal += int64(len(data) / pcm16BytesPerSample)
	return r.emit(false)
}

// Flush returns the output samples left once the input has ended: those at
// or past the last input sample repeat it, as ResamplePCM16 does. A trailing
// odd byte is dropped.
func (r *PCM16StreamResampler) Flush() []byte {
	r.carry = nil
	return r.emit(true)
}

// emit produces output samples up to what the input so far supports. Output
// sample k sits at input position k*from/to.
func (r *PCM16StreamResampler) emit(final bool) []byte {
	total := r.inTotal * r.to / r.from // outputs the whole input yields
	var out []byte
	for r.outIndex < total {
		num := r.outIndex * r.from
		idx, rem := num/r.to, num%r.to
		var v int16
		switch {
		case idx >= r.inTotal-1:
			if !final {
				return r.trim(out)
			}
			v = r.window[r.inTotal-1-r.base]
		default:
			s0 := float64(r.window[idx-r.base])
			s1 := float64(r.window[idx+1-r.base])
			v = int16(s0 + float64(rem)/float64(r.to)*(s1-s0))
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(v)) //nolint:gosec // PCM16
		r.outIndex++
	}
	return r.trim(out)
}

// trim drops input samples no future output sample reads.
func (r *PCM16StreamResampler) trim(out []byte) []byte {
	keepFrom := r.outIndex * r.from / r.to
	if drop := keepFrom - r.base; drop > 0 && drop <= int64(len(r.window)) {
		r.window = r.window[drop:]
		r.base = keepFrom
	}
	if out == nil {
		return []byte{}
	}
	return out
}
