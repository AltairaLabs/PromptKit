package stage

import "time"

// SetResponseVADCheckInterval sets how long s sleeps between checks of a held
// EndOfStream, so tests in package stage_test need not wait out the
// production interval.
func SetResponseVADCheckInterval(s *ResponseVADStage, d time.Duration) { s.checkInterval = d }
