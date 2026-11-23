package persist

// ConfigOf returns the config a recording stage records with. stage's
// realtime-chain test reads it to check the chain built its recorder from the
// host's RecordingStageConfig.
func ConfigOf(rs *RecordingStage) RecordingStageConfig {
	return rs.config
}
