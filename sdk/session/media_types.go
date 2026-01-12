// Package session provides session abstractions for managing conversations.
package session

import "time"

// ImageFrame represents an image frame for streaming.
// Use this with DuplexSession.SendFrame() for realtime video scenarios.
type ImageFrame struct {
	// Data is the raw image data (JPEG, PNG, etc.)
	Data []byte

	// MIMEType is the MIME type of the image (e.g., "image/jpeg")
	MIMEType string

	// Width is the image width in pixels (optional, for metadata)
	Width int

	// Height is the image height in pixels (optional, for metadata)
	Height int

	// FrameNum is the sequence number for ordering frames
	FrameNum int64

	// Timestamp is when the frame was captured
	Timestamp time.Time
}

// VideoChunk represents a video chunk for streaming.
// Use this with DuplexSession.SendVideoChunk() for encoded video segments.
type VideoChunk struct {
	// Data is the encoded video data (H.264, VP8, etc.)
	Data []byte

	// MIMEType is the MIME type of the video (e.g., "video/h264")
	MIMEType string

	// Width is the video width in pixels (optional, for metadata)
	Width int

	// Height is the video height in pixels (optional, for metadata)
	Height int

	// ChunkIndex is the sequence number for ordering chunks
	ChunkIndex int

	// IsKeyFrame indicates if this chunk contains a keyframe
	IsKeyFrame bool

	// Timestamp is when the chunk was created/captured
	Timestamp time.Time

	// Duration is the duration of this video chunk
	Duration time.Duration
}
