package adapters

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainsGlobChars(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{"literal path", "/path/to/file.json", false},
		{"with asterisk", "/path/to/*.json", true},
		{"with question mark", "/path/to/file?.json", true},
		{"with bracket", "/path/to/[abc].json", true},
		{"with double asterisk", "/path/**/file.json", true},
		{"asterisk in filename", "/path/to/file*.json", true},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsGlobChars(tt.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnumerateFiles_SingleFile(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.json")
	require.NoError(t, os.WriteFile(tmpFile, []byte("{}"), 0644))

	// Non-glob path should return single reference
	refs, err := EnumerateFiles(tmpFile, "session")
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, tmpFile, refs[0].ID)
	assert.Equal(t, tmpFile, refs[0].Source)
	assert.Equal(t, "session", refs[0].TypeHint)
}

func TestEnumerateFiles_GlobPattern(t *testing.T) {
	// Create temp directory with multiple files
	tmpDir := t.TempDir()

	files := []string{"test1.json", "test2.json", "test3.json", "other.txt"}
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, f), []byte("{}"), 0644))
	}

	// Glob pattern should return matching files
	pattern := filepath.Join(tmpDir, "*.json")
	refs, err := EnumerateFiles(pattern, "session")
	require.NoError(t, err)
	require.Len(t, refs, 3) // 3 .json files

	// Verify each reference has correct fields
	for _, ref := range refs {
		assert.Contains(t, ref.ID, ".json")
		assert.Equal(t, pattern, ref.Source)
		assert.Equal(t, "session", ref.TypeHint)
	}
}

func TestEnumerateFiles_NoMatches(t *testing.T) {
	tmpDir := t.TempDir()

	// Glob pattern with no matches
	pattern := filepath.Join(tmpDir, "*.nonexistent")
	refs, err := EnumerateFiles(pattern, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files matched")
	assert.Nil(t, refs)
}

func TestEnumerateFiles_InvalidGlobPattern(t *testing.T) {
	// Invalid glob pattern
	refs, err := EnumerateFiles("/path/[invalid", "")
	// filepath.Glob returns error for invalid patterns
	// but we should handle it gracefully
	if err != nil {
		// Either returns "invalid glob pattern" or "no files matched"
		assert.True(t, len(refs) == 0, "should return empty refs on error")
	} else {
		assert.Empty(t, refs)
	}
}

func TestConvertMediaToContent(t *testing.T) {
	tests := []struct {
		name     string
		media    *MediaSource
		validate func(t *testing.T, result *types.MediaContent)
	}{
		{
			name: "converts data source",
			media: &MediaSource{
				MIMEType: "image/png",
				Data:     "base64encodeddata",
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				assert.Equal(t, "image/png", result.MIMEType)
				require.NotNil(t, result.Data)
				assert.Equal(t, "base64encodeddata", *result.Data)
				assert.Nil(t, result.URL)
				assert.Nil(t, result.FilePath)
			},
		},
		{
			name: "converts URI source",
			media: &MediaSource{
				MIMEType: "image/jpeg",
				URI:      "https://example.com/image.jpg",
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				assert.Equal(t, "image/jpeg", result.MIMEType)
				require.NotNil(t, result.URL)
				assert.Equal(t, "https://example.com/image.jpg", *result.URL)
				assert.Nil(t, result.Data)
				assert.Nil(t, result.FilePath)
			},
		},
		{
			name: "converts path source",
			media: &MediaSource{
				MIMEType: "audio/mp3",
				Path:     "/path/to/audio.mp3",
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				assert.Equal(t, "audio/mp3", result.MIMEType)
				require.NotNil(t, result.FilePath)
				assert.Equal(t, "/path/to/audio.mp3", *result.FilePath)
				assert.Nil(t, result.Data)
				assert.Nil(t, result.URL)
			},
		},
		{
			name: "prefers data over URI and path",
			media: &MediaSource{
				MIMEType: "image/png",
				Data:     "data",
				URI:      "https://example.com/image.jpg",
				Path:     "/path/to/file",
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				require.NotNil(t, result.Data)
				assert.Equal(t, "data", *result.Data)
				assert.Nil(t, result.URL)
				assert.Nil(t, result.FilePath)
			},
		},
		{
			name: "prefers URI over path when no data",
			media: &MediaSource{
				MIMEType: "image/png",
				URI:      "https://example.com/image.jpg",
				Path:     "/path/to/file",
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				require.NotNil(t, result.URL)
				assert.Equal(t, "https://example.com/image.jpg", *result.URL)
				assert.Nil(t, result.Data)
				assert.Nil(t, result.FilePath)
			},
		},
		{
			name: "converts size in bytes to KB",
			media: &MediaSource{
				MIMEType: "image/png",
				Data:     "data",
				Size:     2048, // 2KB in bytes
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				require.NotNil(t, result.SizeKB)
				assert.Equal(t, int64(2), *result.SizeKB)
			},
		},
		{
			name: "converts image dimensions",
			media: &MediaSource{
				MIMEType: "image/png",
				Data:     "data",
				Width:    1920,
				Height:   1080,
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				require.NotNil(t, result.Width)
				assert.Equal(t, 1920, *result.Width)
				require.NotNil(t, result.Height)
				assert.Equal(t, 1080, *result.Height)
			},
		},
		{
			name: "converts duration from milliseconds to seconds",
			media: &MediaSource{
				MIMEType: "video/mp4",
				Data:     "data",
				Duration: 5000, // 5 seconds in milliseconds
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				require.NotNil(t, result.Duration)
				assert.Equal(t, 5, *result.Duration)
			},
		},
		{
			name: "handles all fields together",
			media: &MediaSource{
				MIMEType: "video/mp4",
				URI:      "https://example.com/video.mp4",
				Size:     10240, // 10KB
				Width:    1920,
				Height:   1080,
				Duration: 30000, // 30 seconds
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				assert.Equal(t, "video/mp4", result.MIMEType)
				require.NotNil(t, result.URL)
				assert.Equal(t, "https://example.com/video.mp4", *result.URL)
				require.NotNil(t, result.SizeKB)
				assert.Equal(t, int64(10), *result.SizeKB)
				require.NotNil(t, result.Width)
				assert.Equal(t, 1920, *result.Width)
				require.NotNil(t, result.Height)
				assert.Equal(t, 1080, *result.Height)
				require.NotNil(t, result.Duration)
				assert.Equal(t, 30, *result.Duration)
			},
		},
		{
			name: "skips zero values",
			media: &MediaSource{
				MIMEType: "image/png",
				Data:     "data",
				Size:     0,
				Width:    0,
				Height:   0,
				Duration: 0,
			},
			validate: func(t *testing.T, result *types.MediaContent) {
				assert.Nil(t, result.SizeKB)
				assert.Nil(t, result.Width)
				assert.Nil(t, result.Height)
				assert.Nil(t, result.Duration)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMediaToContent(tt.media)
			require.NotNil(t, result)
			tt.validate(t, result)
		})
	}
}
