package interview

import (
	"fmt"
	"sync"
	"time"
)

// InterviewState tracks the current state of an interview
type InterviewState struct {
	mu sync.RWMutex

	// Interview configuration
	Topic          string
	QuestionBank   *QuestionBank
	TotalQuestions int

	// Progress tracking
	CurrentQuestion int
	Scores          []int
	Hints           []bool
	Responses       []string
	StartTime       time.Time
	EndTime         time.Time

	// Session state
	SessionID string
	Status    InterviewStatus
	Mode      InterviewMode
}

// InterviewStatus represents the current status of the interview
type InterviewStatus int

const (
	StatusNotStarted InterviewStatus = iota
	StatusInProgress
	StatusWaitingForResponse
	StatusProcessingResponse
	StatusCompleted
	StatusCancelled
)

// InterviewMode represents the audio processing mode
type InterviewMode int

const (
	ModeASM InterviewMode = iota // Audio Streaming Model (native bidirectional)
	ModeVAD                      // Voice Activity Detection + TTS
)

// String returns the string representation of the status
func (s InterviewStatus) String() string {
	switch s {
	case StatusNotStarted:
		return "Not Started"
	case StatusInProgress:
		return "In Progress"
	case StatusWaitingForResponse:
		return "Waiting for Response"
	case StatusProcessingResponse:
		return "Processing Response"
	case StatusCompleted:
		return "Completed"
	case StatusCancelled:
		return "Canceled"
	default:
		return "Unknown"
	}
}

// String returns the string representation of the mode
func (m InterviewMode) String() string {
	switch m {
	case ModeASM:
		return "ASM (Native Audio)"
	case ModeVAD:
		return "VAD + TTS"
	default:
		return "Unknown"
	}
}

// NewInterviewState creates a new interview state
func NewInterviewState(sessionID string, questionBank *QuestionBank, mode InterviewMode) *InterviewState {
	return &InterviewState{
		Topic:           questionBank.Topic,
		QuestionBank:    questionBank,
		TotalQuestions:  len(questionBank.Questions),
		CurrentQuestion: 0,
		Scores:          make([]int, 0, len(questionBank.Questions)),
		Hints:           make([]bool, 0, len(questionBank.Questions)),
		Responses:       make([]string, 0, len(questionBank.Questions)),
		SessionID:       sessionID,
		Status:          StatusNotStarted,
		Mode:            mode,
	}
}

// Start begins the interview
func (is *InterviewState) Start() {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.Status = StatusInProgress
	is.CurrentQuestion = 1
	is.StartTime = time.Now()
}

// GetCurrentQuestion returns the current question
func (is *InterviewState) GetCurrentQuestion() *Question {
	is.mu.RLock()
	defer is.mu.RUnlock()

	if is.CurrentQuestion < 1 || is.CurrentQuestion > len(is.QuestionBank.Questions) {
		return nil
	}
	return &is.QuestionBank.Questions[is.CurrentQuestion-1]
}

// RecordResponse records a response for the current question
func (is *InterviewState) RecordResponse(response string, score int, usedHint bool) {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.Responses = append(is.Responses, response)
	is.Scores = append(is.Scores, score)
	is.Hints = append(is.Hints, usedHint)
}

// NextQuestion advances to the next question
func (is *InterviewState) NextQuestion() bool {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.CurrentQuestion++
	if is.CurrentQuestion > is.TotalQuestions {
		is.Status = StatusCompleted
		is.EndTime = time.Now()
		return false
	}
	is.Status = StatusInProgress
	return true
}

// Complete marks the interview as completed
func (is *InterviewState) Complete() {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.Status = StatusCompleted
	is.EndTime = time.Now()
}

// Cancel marks the interview as canceled
func (is *InterviewState) Cancel() {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.Status = StatusCancelled
	is.EndTime = time.Now()
}

// GetStatus returns the current status
func (is *InterviewState) GetStatus() InterviewStatus {
	is.mu.RLock()
	defer is.mu.RUnlock()
	return is.Status
}

// SetStatus sets the current status
func (is *InterviewState) SetStatus(status InterviewStatus) {
	is.mu.Lock()
	defer is.mu.Unlock()
	is.Status = status
}

// GetProgress returns the current progress as a percentage
func (is *InterviewState) GetProgress() float64 {
	is.mu.RLock()
	defer is.mu.RUnlock()

	if is.TotalQuestions == 0 {
		return 0
	}
	answered := len(is.Scores)
	return float64(answered) / float64(is.TotalQuestions) * 100
}

// GetTotalScore returns the current total score
func (is *InterviewState) GetTotalScore() int {
	is.mu.RLock()
	defer is.mu.RUnlock()

	total := 0
	for _, s := range is.Scores {
		total += s
	}
	return total
}

// GetMaxScore returns the maximum possible score
func (is *InterviewState) GetMaxScore() int {
	is.mu.RLock()
	defer is.mu.RUnlock()
	return is.TotalQuestions * 10
}

// GetDuration returns the duration of the interview
func (is *InterviewState) GetDuration() time.Duration {
	is.mu.RLock()
	defer is.mu.RUnlock()

	if is.StartTime.IsZero() {
		return 0
	}
	if is.EndTime.IsZero() {
		return time.Since(is.StartTime)
	}
	return is.EndTime.Sub(is.StartTime)
}

// GetVariables returns the variables for the prompt template
func (is *InterviewState) GetVariables() map[string]string {
	is.mu.RLock()
	defer is.mu.RUnlock()

	return map[string]string{
		"topic":            is.Topic,
		"questions":        is.QuestionBank.FormatQuestionsForPrompt(),
		"total_questions":  intToStr(is.TotalQuestions),
		"current_question": intToStr(is.CurrentQuestion),
		"current_score":    intToStr(is.GetTotalScore()),
		"max_score":        intToStr(is.GetMaxScore()),
	}
}

// GetSummary returns a summary of the interview results
func (is *InterviewState) GetSummary() *InterviewSummary {
	is.mu.RLock()
	defer is.mu.RUnlock()

	total := 0
	for _, s := range is.Scores {
		total += s
	}

	hintsUsed := 0
	for _, h := range is.Hints {
		if h {
			hintsUsed++
		}
	}

	percentage := 0.0
	if is.TotalQuestions > 0 {
		percentage = float64(total) / float64(is.TotalQuestions*10) * 100
	}

	grade := calculateGrade(percentage)

	return &InterviewSummary{
		Topic:          is.Topic,
		TotalScore:     total,
		MaxScore:       is.TotalQuestions * 10,
		Percentage:     percentage,
		Grade:          grade,
		QuestionsAsked: len(is.Scores),
		HintsUsed:      hintsUsed,
		Duration:       is.GetDuration(),
		QuestionScores: is.Scores,
	}
}

// InterviewSummary holds the final interview results
type InterviewSummary struct {
	Topic          string
	TotalScore     int
	MaxScore       int
	Percentage     float64
	Grade          string
	QuestionsAsked int
	HintsUsed      int
	Duration       time.Duration
	QuestionScores []int
}

func calculateGrade(percentage float64) string {
	switch {
	case percentage >= 90:
		return "A"
	case percentage >= 80:
		return "B"
	case percentage >= 70:
		return "C"
	case percentage >= 60:
		return "D"
	default:
		return "F"
	}
}

func intToStr(i int) string {
	return fmt.Sprintf("%d", i)
}
