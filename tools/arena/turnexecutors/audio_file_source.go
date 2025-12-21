package turnexecutors

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// AudioFileSource provides streaming audio from a file for duplex mode.
// It reads audio files and provides chunks of raw PCM audio data suitable
// for streaming to providers like Gemini Live API.
type AudioFileSource struct {
	file       *os.File
	reader     io.Reader
	sampleRate int
	channels   int
	bitDepth   int
	dataStart  int64 // Position where audio data starts
	dataSize   int64 // Size of audio data
	format     AudioFormat
}

// AudioFormat represents the audio encoding format.
type AudioFormat int

const (
	// AudioFormatPCM16 is 16-bit signed PCM (little-endian).
	AudioFormatPCM16 AudioFormat = iota
	// AudioFormatPCM24 is 24-bit signed PCM (little-endian).
	AudioFormatPCM24
	// AudioFormatPCM32 is 32-bit signed PCM (little-endian).
	AudioFormatPCM32
	// AudioFormatFloat32 is 32-bit float PCM.
	AudioFormatFloat32
)

// Default audio parameters
const (
	defaultSampleRate = 16000
	defaultChannels   = 1
	defaultBitDepth   = 16
)

// Audio format constants
const (
	// Minimum fmt chunk size
	minFmtChunkSize = 16
	// WAV audio format codes
	wavFormatPCM  = 1
	wavFormatIEEE = 3
	// Bit depths
	bitDepth16 = 16
	bitDepth24 = 24
	bitDepth32 = 32
	// Byte sizes
	bytesPerSample16 = 2
	bytesPerSample24 = 3
	bytesPerSample32 = 4
	bitsPerByte      = 8
	// Audio sample conversion constants
	maxInt16AsFloat = 32767
	signExtendMask  = 0xFFFFFF
	signBitMask     = 0x800000
)

// NewAudioFileSource creates a new audio file source for streaming.
// It supports WAV files and will automatically parse headers to determine format.
// The filePath can be absolute or relative to baseDir.
func NewAudioFileSource(filePath, baseDir string) (*AudioFileSource, error) {
	// Resolve path
	resolvedPath := filePath
	if !filepath.IsAbs(filePath) && baseDir != "" {
		resolvedPath = filepath.Join(baseDir, filePath)
	}

	// Open file
	//nolint:gosec // File path comes from scenario configuration, not user input
	file, err := os.Open(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}

	// Detect format based on extension
	ext := strings.ToLower(filepath.Ext(resolvedPath))
	source := &AudioFileSource{
		file:       file,
		sampleRate: defaultSampleRate,
		channels:   defaultChannels,
		bitDepth:   defaultBitDepth,
		format:     AudioFormatPCM16,
	}

	switch ext {
	case ".wav":
		if err := source.parseWAVHeader(); err != nil {
			_ = file.Close() // Intentionally ignoring close error during error path
			return nil, fmt.Errorf("failed to parse WAV header: %w", err)
		}
	case ".pcm", ".raw":
		// Raw PCM - use defaults, data starts at beginning
		source.reader = file
	default:
		_ = file.Close() // Intentionally ignoring close error during error path
		return nil, fmt.Errorf("unsupported audio format: %s (supported: .wav, .pcm, .raw)", ext)
	}

	return source, nil
}

// parseWAVHeader reads and parses a WAV file header.
func (s *AudioFileSource) parseWAVHeader() error {
	if err := s.validateRIFFHeader(); err != nil {
		return err
	}
	return s.parseWAVChunks()
}

// validateRIFFHeader reads and validates the RIFF/WAVE header.
func (s *AudioFileSource) validateRIFFHeader() error {
	var riffHeader [12]byte
	if _, err := io.ReadFull(s.file, riffHeader[:]); err != nil {
		return fmt.Errorf("failed to read RIFF header: %w", err)
	}

	if string(riffHeader[0:4]) != "RIFF" {
		return errors.New("not a valid RIFF file")
	}
	if string(riffHeader[8:12]) != "WAVE" {
		return errors.New("not a valid WAVE file")
	}
	return nil
}

// parseWAVChunks parses WAV chunks until the data chunk is found.
func (s *AudioFileSource) parseWAVChunks() error {
	for {
		chunkID, chunkSize, err := s.readChunkHeader()
		if err != nil {
			return err
		}

		done, err := s.processChunk(chunkID, chunkSize)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// readChunkHeader reads the next chunk header from the file.
func (s *AudioFileSource) readChunkHeader() (chunkID string, chunkSize uint32, err error) {
	var chunkHeader [8]byte
	if _, err = io.ReadFull(s.file, chunkHeader[:]); err != nil {
		if err == io.EOF {
			return "", 0, errors.New("data chunk not found")
		}
		return "", 0, fmt.Errorf("failed to read chunk header: %w", err)
	}
	chunkID = string(chunkHeader[0:4])
	chunkSize = binary.LittleEndian.Uint32(chunkHeader[4:8])
	return chunkID, chunkSize, nil
}

// processChunk processes a single WAV chunk. Returns true if the data chunk was found.
func (s *AudioFileSource) processChunk(chunkID string, chunkSize uint32) (bool, error) {
	switch chunkID {
	case "fmt ":
		if err := s.parseFmtChunk(int64(chunkSize)); err != nil {
			return false, err
		}
	case "data":
		s.dataSize = int64(chunkSize)
		pos, _ := s.file.Seek(0, io.SeekCurrent)
		s.dataStart = pos
		s.reader = io.LimitReader(s.file, s.dataSize)
		return true, nil
	default:
		if _, err := s.file.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
			return false, fmt.Errorf("failed to skip chunk %s: %w", chunkID, err)
		}
	}
	return false, nil
}

// parseFmtChunk parses the fmt chunk of a WAV file.
func (s *AudioFileSource) parseFmtChunk(size int64) error {
	if size < minFmtChunkSize {
		return errors.New("fmt chunk too small")
	}

	// Read format data
	fmtData := make([]byte, size)
	if _, err := io.ReadFull(s.file, fmtData); err != nil {
		return fmt.Errorf("failed to read fmt chunk: %w", err)
	}

	// Parse format fields
	audioFormat := binary.LittleEndian.Uint16(fmtData[0:2])
	s.channels = int(binary.LittleEndian.Uint16(fmtData[2:4]))
	s.sampleRate = int(binary.LittleEndian.Uint32(fmtData[4:8]))
	// bytes 8-11: byte rate (skip)
	// bytes 12-13: block align (skip)
	s.bitDepth = int(binary.LittleEndian.Uint16(fmtData[14:16]))

	// Determine format
	switch audioFormat {
	case wavFormatPCM:
		switch s.bitDepth {
		case bitDepth16:
			s.format = AudioFormatPCM16
		case bitDepth24:
			s.format = AudioFormatPCM24
		case bitDepth32:
			s.format = AudioFormatPCM32
		default:
			return fmt.Errorf("unsupported PCM bit depth: %d", s.bitDepth)
		}
	case wavFormatIEEE:
		if s.bitDepth == bitDepth32 {
			s.format = AudioFormatFloat32
		} else {
			return fmt.Errorf("unsupported float bit depth: %d", s.bitDepth)
		}
	default:
		return fmt.Errorf("unsupported audio format code: %d (only PCM and IEEE Float supported)", audioFormat)
	}

	return nil
}

// ReadChunk reads up to size bytes of audio data.
// Returns the audio data as raw bytes suitable for streaming.
// Returns io.EOF when all data has been read.
func (s *AudioFileSource) ReadChunk(size int) ([]byte, error) {
	if s.reader == nil {
		return nil, errors.New("audio source not initialized")
	}

	buf := make([]byte, size)
	n, err := s.reader.Read(buf)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read audio data: %w", err)
	}
	if n == 0 {
		return nil, io.EOF
	}

	// If format is not PCM16, convert to PCM16
	if s.format != AudioFormatPCM16 {
		return s.convertToPCM16(buf[:n])
	}

	return buf[:n], nil
}

// convertToPCM16 converts audio data to 16-bit PCM.
func (s *AudioFileSource) convertToPCM16(data []byte) ([]byte, error) {
	switch s.format {
	case AudioFormatPCM16:
		return data, nil
	case AudioFormatPCM24:
		return s.convert24To16(data), nil
	case AudioFormatPCM32:
		return s.convert32To16(data), nil
	case AudioFormatFloat32:
		return s.convertFloat32To16(data), nil
	default:
		return data, nil
	}
}

// convert24To16 converts 24-bit PCM to 16-bit PCM.
func (s *AudioFileSource) convert24To16(data []byte) []byte {
	samples := len(data) / bytesPerSample24
	out := make([]byte, samples*bytesPerSample16)

	for i := 0; i < samples; i++ {
		// Read 24-bit sample (little-endian, signed)
		sample := int32(data[i*bytesPerSample24]) |
			int32(data[i*bytesPerSample24+1])<<bitsPerByte |
			int32(data[i*bytesPerSample24+2])<<(bytesPerSample16*bitsPerByte)
		// Sign extend
		if sample&signBitMask != 0 {
			sample |= ^signExtendMask
		}
		// Convert to 16-bit (intentional truncation for audio downsampling)
		sample16 := int16(sample >> bitsPerByte) //nolint:gosec // Intentional audio truncation
		//nolint:gosec // Intentional signed to unsigned for binary encoding
		binary.LittleEndian.PutUint16(out[i*bytesPerSample16:], uint16(sample16))
	}

	return out
}

// convert32To16 converts 32-bit PCM to 16-bit PCM.
func (s *AudioFileSource) convert32To16(data []byte) []byte {
	samples := len(data) / bytesPerSample32
	out := make([]byte, samples*bytesPerSample16)

	for i := 0; i < samples; i++ {
		//nolint:gosec // Intentional unsigned to signed for audio processing
		sample := int32(binary.LittleEndian.Uint32(data[i*bytesPerSample32:]))
		// Convert to 16-bit (intentional truncation for audio downsampling)
		sample16 := int16(sample >> (bytesPerSample16 * bitsPerByte)) //nolint:gosec // Intentional audio truncation
		//nolint:gosec // Intentional signed to unsigned for binary encoding
		binary.LittleEndian.PutUint16(out[i*bytesPerSample16:], uint16(sample16))
	}

	return out
}

// convertFloat32To16 converts 32-bit float PCM to 16-bit PCM.
func (s *AudioFileSource) convertFloat32To16(data []byte) []byte {
	samples := len(data) / bytesPerSample32
	out := make([]byte, samples*bytesPerSample16)
	reader := bytes.NewReader(data)

	for i := 0; i < samples; i++ {
		var f float32
		if err := binary.Read(reader, binary.LittleEndian, &f); err != nil {
			break
		}
		// Clamp to [-1, 1] and convert to 16-bit
		if f > 1.0 {
			f = 1.0
		} else if f < -1.0 {
			f = -1.0
		}
		sample16 := int16(f * maxInt16AsFloat)
		//nolint:gosec // Intentional signed to unsigned for binary encoding
		binary.LittleEndian.PutUint16(out[i*bytesPerSample16:], uint16(sample16))
	}

	return out
}

// SampleRate returns the sample rate of the audio.
func (s *AudioFileSource) SampleRate() int {
	return s.sampleRate
}

// Channels returns the number of audio channels.
func (s *AudioFileSource) Channels() int {
	return s.channels
}

// BitDepth returns the original bit depth of the audio.
func (s *AudioFileSource) BitDepth() int {
	return s.bitDepth
}

// Format returns the audio format.
func (s *AudioFileSource) Format() AudioFormat {
	return s.format
}

// Reset seeks back to the beginning of the audio data.
func (s *AudioFileSource) Reset() error {
	if s.file == nil {
		return errors.New("audio source closed")
	}

	_, err := s.file.Seek(s.dataStart, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to data start: %w", err)
	}

	// Re-create the reader
	if s.dataSize > 0 {
		s.reader = io.LimitReader(s.file, s.dataSize)
	} else {
		s.reader = s.file
	}

	return nil
}

// Close closes the audio file.
func (s *AudioFileSource) Close() error {
	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		s.reader = nil
		return err
	}
	return nil
}

// Duration returns the estimated duration of the audio in seconds.
func (s *AudioFileSource) Duration() float64 {
	if s.dataSize == 0 || s.sampleRate == 0 || s.channels == 0 {
		return 0
	}
	bytesPerSample := s.bitDepth / bitsPerByte
	if bytesPerSample == 0 {
		bytesPerSample = bytesPerSample16 // Default to 16-bit
	}
	samples := s.dataSize / int64(bytesPerSample*s.channels)
	return float64(samples) / float64(s.sampleRate)
}
