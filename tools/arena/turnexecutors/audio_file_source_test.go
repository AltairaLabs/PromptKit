package turnexecutors

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNewAudioFileSource_WAV(t *testing.T) {
	// Create a minimal WAV file for testing
	wavPath := createTestWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	// Verify parsed format
	if source.SampleRate() != 16000 {
		t.Errorf("Expected sample rate 16000, got %d", source.SampleRate())
	}
	if source.Channels() != 1 {
		t.Errorf("Expected 1 channel, got %d", source.Channels())
	}
	if source.BitDepth() != 16 {
		t.Errorf("Expected bit depth 16, got %d", source.BitDepth())
	}
	if source.Format() != AudioFormatPCM16 {
		t.Errorf("Expected AudioFormatPCM16, got %d", source.Format())
	}
}

func TestAudioFileSource_ReadChunk(t *testing.T) {
	wavPath := createTestWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	// Read first chunk
	chunk, err := source.ReadChunk(640)
	if err != nil {
		t.Fatalf("Failed to read chunk: %v", err)
	}

	if len(chunk) == 0 {
		t.Error("Expected non-empty chunk")
	}

	// Continue reading until EOF
	totalBytes := len(chunk)
	for {
		chunk, err = source.ReadChunk(640)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Unexpected error reading chunk: %v", err)
		}
		totalBytes += len(chunk)
	}

	// Verify we read all audio data (1024 samples = 2048 bytes for 16-bit)
	if totalBytes != 2048 {
		t.Errorf("Expected 2048 bytes total, got %d", totalBytes)
	}
}

func TestAudioFileSource_Reset(t *testing.T) {
	wavPath := createTestWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	// Read all data
	for {
		_, err := source.ReadChunk(640)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Error reading: %v", err)
		}
	}

	// Reset and read again
	if err := source.Reset(); err != nil {
		t.Fatalf("Failed to reset: %v", err)
	}

	chunk, err := source.ReadChunk(640)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read after reset: %v", err)
	}
	if len(chunk) == 0 {
		t.Error("Expected non-empty chunk after reset")
	}
}

func TestAudioFileSource_Duration(t *testing.T) {
	wavPath := createTestWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	// 1024 samples at 16000 Hz = 0.064 seconds
	duration := source.Duration()
	expected := 0.064
	if duration < expected-0.001 || duration > expected+0.001 {
		t.Errorf("Expected duration ~%f, got %f", expected, duration)
	}
}

func TestAudioFileSource_RelativePath(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	audioDir := filepath.Join(tmpDir, "audio")
	if err := os.MkdirAll(audioDir, 0755); err != nil {
		t.Fatalf("Failed to create audio directory: %v", err)
	}

	// Create WAV file in subdirectory
	wavPath := filepath.Join(audioDir, "test.wav")
	createTestWAVFileAt(t, wavPath)

	// Open with relative path
	source, err := NewAudioFileSource("audio/test.wav", tmpDir)
	if err != nil {
		t.Fatalf("Failed to open with relative path: %v", err)
	}
	defer source.Close()

	if source.SampleRate() != 16000 {
		t.Errorf("Expected sample rate 16000, got %d", source.SampleRate())
	}
}

func TestAudioFileSource_UnsupportedFormat(t *testing.T) {
	// Create a temp file with unsupported extension
	tmpFile, err := os.CreateTemp("", "test*.mp3")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	_, err = NewAudioFileSource(tmpFile.Name(), "")
	if err == nil {
		t.Error("Expected error for unsupported format")
	}
}

func TestAudioFileSource_InvalidWAV(t *testing.T) {
	// Create a file that's not a valid WAV
	tmpFile, err := os.CreateTemp("", "test*.wav")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Write([]byte("not a valid wav file"))
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	_, err = NewAudioFileSource(tmpFile.Name(), "")
	if err == nil {
		t.Error("Expected error for invalid WAV")
	}
}

// createTestWAVFile creates a minimal valid WAV file for testing
func createTestWAVFile(t *testing.T) string {
	tmpFile, err := os.CreateTemp("", "test*.wav")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	path := tmpFile.Name()
	tmpFile.Close()

	createTestWAVFileAt(t, path)
	return path
}

// createTestWAVFileAt creates a minimal valid WAV file at the specified path
func createTestWAVFileAt(t *testing.T, path string) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer f.Close()

	// Create a minimal WAV file:
	// - 16kHz sample rate
	// - Mono
	// - 16-bit PCM
	// - 1024 samples of silence (2048 bytes)

	sampleRate := uint32(16000)
	channels := uint16(1)
	bitsPerSample := uint16(16)
	dataSize := uint32(2048) // 1024 samples * 2 bytes

	// RIFF header
	f.Write([]byte("RIFF"))
	writeUint32LE(f, 36+dataSize) // file size - 8
	f.Write([]byte("WAVE"))

	// fmt chunk
	f.Write([]byte("fmt "))
	writeUint32LE(f, 16)                                        // chunk size
	writeUint16LE(f, 1)                                         // audio format (PCM)
	writeUint16LE(f, channels)                                  // channels
	writeUint32LE(f, sampleRate)                                // sample rate
	writeUint32LE(f, sampleRate*uint32(channels*bitsPerSample/8)) // byte rate
	writeUint16LE(f, channels*bitsPerSample/8)                    // block align
	writeUint16LE(f, bitsPerSample)                               // bits per sample

	// data chunk
	f.Write([]byte("data"))
	writeUint32LE(f, dataSize)

	// Write silence (zeros)
	silence := make([]byte, dataSize)
	f.Write(silence)
}

func writeUint16LE(f *os.File, v uint16) {
	f.Write([]byte{byte(v), byte(v >> 8)})
}

func writeUint32LE(f *os.File, v uint32) {
	f.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

func TestAudioFileSource_ConvertToPCM16(t *testing.T) {
	// Create a test WAV file
	wavPath := createTestWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	// Test convert24To16
	input24 := []byte{0x00, 0x00, 0x80} // -32768 in 24-bit
	result16 := source.convert24To16(input24)
	if len(result16) != 2 {
		t.Errorf("Expected 2 bytes for 16-bit, got %d", len(result16))
	}

	// Test convert32To16
	input32 := []byte{0x00, 0x00, 0x00, 0x80} // -2147483648 in 32-bit
	result16 = source.convert32To16(input32)
	if len(result16) != 2 {
		t.Errorf("Expected 2 bytes for 16-bit, got %d", len(result16))
	}

	// Test convertFloat32To16
	// Create a simple float32 value
	inputFloat := []byte{0x00, 0x00, 0x00, 0x00} // 0.0
	result16 = source.convertFloat32To16(inputFloat)
	if len(result16) != 2 {
		t.Errorf("Expected 2 bytes for 16-bit, got %d", len(result16))
	}
}

func TestAudioFileSource_PCM24File(t *testing.T) {
	// Create a 24-bit WAV file
	wavPath := createTest24BitWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	if source.Format() != AudioFormatPCM24 {
		t.Errorf("Expected AudioFormatPCM24, got %d", source.Format())
	}
	if source.BitDepth() != 24 {
		t.Errorf("Expected bit depth 24, got %d", source.BitDepth())
	}

	// Read and convert a chunk
	chunk, err := source.ReadChunk(640)
	if err != nil {
		t.Fatalf("Failed to read chunk: %v", err)
	}
	// After conversion to 16-bit, output should be 2/3 the size
	if len(chunk) == 0 {
		t.Error("Expected non-empty chunk after conversion")
	}
}

func TestAudioFileSource_PCM32File(t *testing.T) {
	wavPath := createTest32BitWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	if source.Format() != AudioFormatPCM32 {
		t.Errorf("Expected AudioFormatPCM32, got %d", source.Format())
	}

	chunk, err := source.ReadChunk(640)
	if err != nil {
		t.Fatalf("Failed to read chunk: %v", err)
	}
	if len(chunk) == 0 {
		t.Error("Expected non-empty chunk after conversion")
	}
}

func TestAudioFileSource_Float32File(t *testing.T) {
	wavPath := createTestFloat32WAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	if source.Format() != AudioFormatFloat32 {
		t.Errorf("Expected AudioFormatFloat32, got %d", source.Format())
	}

	chunk, err := source.ReadChunk(640)
	if err != nil {
		t.Fatalf("Failed to read chunk: %v", err)
	}
	if len(chunk) == 0 {
		t.Error("Expected non-empty chunk after conversion")
	}
}

func TestAudioFileSource_RawPCMFile(t *testing.T) {
	// Create a raw PCM file
	tmpFile, err := os.CreateTemp("", "test*.pcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Write(make([]byte, 1024))
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	source, err := NewAudioFileSource(tmpFile.Name(), "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	defer source.Close()

	// Raw PCM uses defaults
	if source.SampleRate() != 16000 {
		t.Errorf("Expected default sample rate 16000, got %d", source.SampleRate())
	}
}

func TestAudioFileSource_ReadChunkNilReader(t *testing.T) {
	wavPath := createTestWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	source.reader = nil

	_, err = source.ReadChunk(640)
	if err == nil {
		t.Error("Expected error when reader is nil")
	}
	source.Close()
}

func TestAudioFileSource_ResetClosed(t *testing.T) {
	wavPath := createTestWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}
	source.Close()

	err = source.Reset()
	if err == nil {
		t.Error("Expected error when resetting closed source")
	}
}

func TestAudioFileSource_CloseIdempotent(t *testing.T) {
	wavPath := createTestWAVFile(t)
	defer os.Remove(wavPath)

	source, err := NewAudioFileSource(wavPath, "")
	if err != nil {
		t.Fatalf("Failed to create audio source: %v", err)
	}

	// Close twice should not panic
	source.Close()
	source.Close()
}

func createTest24BitWAVFile(t *testing.T) string {
	tmpFile, err := os.CreateTemp("", "test*.wav")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	path := tmpFile.Name()
	tmpFile.Close()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer f.Close()

	sampleRate := uint32(16000)
	channels := uint16(1)
	bitsPerSample := uint16(24)
	dataSize := uint32(3072) // 1024 samples * 3 bytes

	f.Write([]byte("RIFF"))
	writeUint32LE(f, 36+dataSize)
	f.Write([]byte("WAVE"))
	f.Write([]byte("fmt "))
	writeUint32LE(f, 16)
	writeUint16LE(f, 1) // PCM
	writeUint16LE(f, channels)
	writeUint32LE(f, sampleRate)
	writeUint32LE(f, sampleRate*uint32(channels*bitsPerSample/8))
	writeUint16LE(f, channels*bitsPerSample/8)
	writeUint16LE(f, bitsPerSample)
	f.Write([]byte("data"))
	writeUint32LE(f, dataSize)
	f.Write(make([]byte, dataSize))

	return path
}

func createTest32BitWAVFile(t *testing.T) string {
	tmpFile, err := os.CreateTemp("", "test*.wav")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	path := tmpFile.Name()
	tmpFile.Close()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer f.Close()

	sampleRate := uint32(16000)
	channels := uint16(1)
	bitsPerSample := uint16(32)
	dataSize := uint32(4096) // 1024 samples * 4 bytes

	f.Write([]byte("RIFF"))
	writeUint32LE(f, 36+dataSize)
	f.Write([]byte("WAVE"))
	f.Write([]byte("fmt "))
	writeUint32LE(f, 16)
	writeUint16LE(f, 1) // PCM
	writeUint16LE(f, channels)
	writeUint32LE(f, sampleRate)
	writeUint32LE(f, sampleRate*uint32(channels*bitsPerSample/8))
	writeUint16LE(f, channels*bitsPerSample/8)
	writeUint16LE(f, bitsPerSample)
	f.Write([]byte("data"))
	writeUint32LE(f, dataSize)
	f.Write(make([]byte, dataSize))

	return path
}

func createTestFloat32WAVFile(t *testing.T) string {
	tmpFile, err := os.CreateTemp("", "test*.wav")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	path := tmpFile.Name()
	tmpFile.Close()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer f.Close()

	sampleRate := uint32(16000)
	channels := uint16(1)
	bitsPerSample := uint16(32)
	dataSize := uint32(4096) // 1024 samples * 4 bytes

	f.Write([]byte("RIFF"))
	writeUint32LE(f, 36+dataSize)
	f.Write([]byte("WAVE"))
	f.Write([]byte("fmt "))
	writeUint32LE(f, 16)
	writeUint16LE(f, 3) // IEEE Float
	writeUint16LE(f, channels)
	writeUint32LE(f, sampleRate)
	writeUint32LE(f, sampleRate*uint32(channels*bitsPerSample/8))
	writeUint16LE(f, channels*bitsPerSample/8)
	writeUint16LE(f, bitsPerSample)
	f.Write([]byte("data"))
	writeUint32LE(f, dataSize)
	f.Write(make([]byte, dataSize))

	return path
}
