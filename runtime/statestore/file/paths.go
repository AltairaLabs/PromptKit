package file

import (
	"path/filepath"
)

const (
	convDirPrefix     = "conv-"
	stateFilename     = "state.json"
	messagesFilename  = "messages.jsonl"
	summariesFilename = "summaries.jsonl"
)

// convDir returns the per-conversation directory: <root>/conv-<id>/.
func (s *Store) convDir(id string) string {
	return filepath.Join(s.root, convDirPrefix+id)
}

func (s *Store) stateFile(id string) string {
	return filepath.Join(s.convDir(id), stateFilename)
}

func (s *Store) messagesFile(id string) string {
	return filepath.Join(s.convDir(id), messagesFilename)
}

func (s *Store) summariesFile(id string) string {
	return filepath.Join(s.convDir(id), summariesFilename)
}
