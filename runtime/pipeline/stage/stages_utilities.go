package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// DebugStage logs StreamElements for debugging pipeline state.
// Useful for development and troubleshooting.
type DebugStage struct {
	BaseStage
	stageName string
}

// NewDebugStage creates a debug stage that logs elements at a specific pipeline location.
func NewDebugStage(stageName string) *DebugStage {
	return &DebugStage{
		BaseStage: NewBaseStage("debug_"+stageName, StageTypeTransform),
		stageName: stageName,
	}
}

// Process logs each element as it passes through (passthrough transform).
func (s *DebugStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	logger.Warn("Debug stage active in pipeline", "stage", s.stageName)

	for elem := range input {
		// Log element snapshot
		s.logElement(&elem, "processing")

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// logElement creates a JSON snapshot of the element and logs it.
func (s *DebugStage) logElement(elem *StreamElement, timing string) {
	snapshot := map[string]interface{}{
		"stage":  s.stageName,
		"timing": timing,
	}

	if elem.Text != nil {
		preview := *elem.Text
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		snapshot["text"] = preview
	}

	if elem.Message != nil {
		snapshot["message"] = map[string]interface{}{
			"role":        elem.Message.Role,
			"content_len": len(elem.Message.Content),
			"tool_calls":  len(elem.Message.ToolCalls),
			"source":      elem.Message.Source,
		}
	}

	if elem.Audio != nil {
		snapshot["audio"] = map[string]interface{}{
			"sample_rate": elem.Audio.SampleRate,
			"samples_len": len(elem.Audio.Samples),
			"format":      elem.Audio.Format,
		}
	}

	if elem.Error != nil {
		snapshot["error"] = elem.Error.Error()
	}

	if len(elem.Metadata) > 0 {
		snapshot["metadata_keys"] = getKeys(elem.Metadata)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		logger.Debug("Failed to marshal element", "error", err)
		return
	}

	logger.Debug(fmt.Sprintf("🐛 [%s:%s] StreamElement:\n%s", s.stageName, timing, string(data)))
}

// getKeys extracts keys from a map for logging.
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TemplateStage substitutes {{variable}} placeholders in messages and metadata.
//
// This stage reads variables from the element's metadata["variables"] map and
// replaces all occurrences of {{variable_name}} in:
//   - metadata["system_prompt"] - the system prompt for the LLM
//   - message.Content - the message text content
//   - message.Parts[].Text - individual content parts
//
// Variables are typically set by:
//   - PromptAssemblyStage (from base_variables in config)
//   - VariableProviderStage (from dynamic variable providers)
//
// Example:
//
//	Input: "Hello {{name}}, the topic is {{topic}}"
//	Variables: {"name": "Alice", "topic": "AI"}
//	Output: "Hello Alice, the topic is AI"
//
// This is a Transform stage: 1 input element → 1 output element
type TemplateStage struct {
	BaseStage
}

// NewTemplateStage creates a template substitution stage.
func NewTemplateStage() *TemplateStage {
	return &TemplateStage{
		BaseStage: NewBaseStage("template", StageTypeTransform),
	}
}

// Process substitutes variables in messages and system prompt metadata.
func (s *TemplateStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		s.substituteElement(&elem)

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// substituteElement performs variable substitution on a single element.
func (s *TemplateStage) substituteElement(elem *StreamElement) {
	// Get variables from metadata if available
	vars, ok := elem.Metadata["variables"].(map[string]string)
	if !ok || vars == nil {
		return
	}

	// Substitute in system prompt if present in metadata
	if systemPrompt, ok := elem.Metadata["system_prompt"].(string); ok {
		elem.Metadata["system_prompt"] = s.substituteVariables(systemPrompt, vars)
	}

	// Substitute in message content if message element
	if elem.Message != nil {
		s.substituteMessage(elem.Message, vars)
	}
}

// substituteMessage performs variable substitution on message content and parts.
func (s *TemplateStage) substituteMessage(msg *types.Message, vars map[string]string) {
	msg.Content = s.substituteVariables(msg.Content, vars)

	for i := range msg.Parts {
		if msg.Parts[i].Text != nil {
			text := s.substituteVariables(*msg.Parts[i].Text, vars)
			msg.Parts[i].Text = &text
		}
	}
}

// substituteVariables replaces {{variable}} placeholders with values.
func (s *TemplateStage) substituteVariables(text string, vars map[string]string) string {
	result := text
	for varName, varValue := range vars {
		placeholder := "{{" + varName + "}}"
		result = strings.ReplaceAll(result, placeholder, varValue)
	}
	return result
}

// VariableProviderStage resolves variables from dynamic providers and adds them to metadata.
//
// This stage calls each registered variable provider to fetch dynamic variables
// (e.g., from environment, external services, databases) and merges them into
// the element's metadata["variables"] map for use by TemplateStage.
//
// Provider resolution order:
//  1. Variables from earlier stages (e.g., PromptAssemblyStage base_variables)
//  2. Each provider is called in sequence; later providers can override earlier values
//
// Error handling:
//   - If any provider fails, the stage returns an error and aborts the pipeline
//   - This ensures variable resolution failures are surfaced early
//
// Example providers:
//   - Environment variable provider: reads from OS environment
//   - Config provider: reads from configuration files
//   - External API provider: fetches user context from external services
//
// This is a Transform stage: 1 input element → 1 output element (with enriched metadata)
type VariableProviderStage struct {
	BaseStage
	providers []variables.Provider
}

// NewVariableProviderStage creates a variable provider stage.
func NewVariableProviderStage(providers ...variables.Provider) *VariableProviderStage {
	return &VariableProviderStage{
		BaseStage: NewBaseStage("variable_provider", StageTypeTransform),
		providers: providers,
	}
}

// Process resolves variables from all providers and merges them into element metadata.
func (s *VariableProviderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	// Resolve variables from all providers once
	allVars := make(map[string]string)
	for _, provider := range s.providers {
		vars, err := provider.Provide(ctx)
		if err != nil {
			logger.Error("Variable provider failed", "provider", provider.Name(), "error", err)
			return fmt.Errorf("variable provider %s failed: %w", provider.Name(), err)
		}

		// Merge (later providers override earlier ones)
		for k, v := range vars {
			allVars[k] = v
		}
	}

	// Add resolved variables to each element's metadata
	for elem := range input {
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}

		// Merge with existing variables
		if existingVars, ok := elem.Metadata["variables"].(map[string]string); ok {
			// Merge existing with new (providers override)
			for k, v := range allVars {
				existingVars[k] = v
			}
			elem.Metadata["variables"] = existingVars
		} else {
			// Set new variables
			elem.Metadata["variables"] = allVars
		}

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// MediaExternalizerConfig configures media externalization behavior.
type MediaExternalizerConfig struct {
	Enabled         bool
	StorageService  storage.MediaStorageService
	SizeThresholdKB int64
	DefaultPolicy   string
	RunID           string
	SessionID       string
	ConversationID  string
}

// MediaExternalizerStage externalizes large media content to external storage.
//
// When messages contain large inline media (images, audio, video), this stage
// moves the data to external storage and replaces it with a storage reference.
// This reduces memory usage and allows for media lifecycle management.
//
// Behavior:
//   - Skipped if Enabled=false or no StorageService configured
//   - Only externalizes media exceeding SizeThresholdKB (base64 size)
//   - Preserves media.StorageReference if already externalized
//   - Clears media.Data after successful externalization
//
// Configuration:
//   - Enabled: master switch for externalization
//   - SizeThresholdKB: minimum size to externalize (0 = externalize all)
//   - StorageService: where to store media (S3, GCS, local filesystem, etc.)
//   - DefaultPolicy: retention policy name for stored media
//
// This is a Transform stage: 1 input element → 1 output element (with externalized media)
type MediaExternalizerStage struct {
	BaseStage
	config *MediaExternalizerConfig
}

// NewMediaExternalizerStage creates a media externalizer stage.
func NewMediaExternalizerStage(config *MediaExternalizerConfig) *MediaExternalizerStage {
	return &MediaExternalizerStage{
		BaseStage: NewBaseStage("media_externalizer", StageTypeTransform),
		config:    config,
	}
}

// Process externalizes media from messages if they exceed size threshold.
func (s *MediaExternalizerStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	// Skip if disabled or no storage service
	if !s.config.Enabled || s.config.StorageService == nil {
		// Pass through without modification
		for elem := range input {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	messageIdx := 0
	for elem := range input {
		// Externalize media in message if present
		if elem.Message != nil {
			if err := s.externalizeMessageMedia(ctx, elem.Message, messageIdx); err != nil {
				logger.Error("Failed to externalize media", "error", err)
				elem.Error = err
			}
			messageIdx++
		}

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// externalizeMessageMedia externalizes media from message parts.
func (s *MediaExternalizerStage) externalizeMessageMedia(
	ctx context.Context,
	msg *types.Message,
	messageIdx int,
) error {
	for partIdx := range msg.Parts {
		part := &msg.Parts[partIdx]

		// Skip non-media parts
		if part.Media == nil {
			continue
		}

		// Externalize this media
		if err := s.externalizeMedia(ctx, part.Media, messageIdx, partIdx); err != nil {
			return fmt.Errorf("failed to externalize media at message %d, part %d: %w", messageIdx, partIdx, err)
		}
	}

	return nil
}

// externalizeMedia moves media content to external storage if it exceeds threshold.
func (s *MediaExternalizerStage) externalizeMedia(
	ctx context.Context,
	media *types.MediaContent,
	messageIdx, partIdx int,
) error {
	// Skip if already externalized
	if media.StorageReference != nil {
		return nil
	}

	// Skip if no inline data
	if media.Data == nil || *media.Data == "" {
		return nil
	}

	// Check size threshold
	if s.config.SizeThresholdKB > 0 {
		// Estimate size from base64 data (base64 is ~4/3 original size)
		estimatedSizeKB := int64(len(*media.Data) * 3 / 4 / 1024)
		if estimatedSizeKB < s.config.SizeThresholdKB {
			// Too small, keep inline
			return nil
		}
	}

	// Build metadata for storage
	metadata := &storage.MediaMetadata{
		RunID:          s.config.RunID,
		SessionID:      s.config.SessionID,
		ConversationID: s.config.ConversationID,
		MessageIdx:     messageIdx,
		PartIdx:        partIdx,
		MIMEType:       media.MIMEType,
		Timestamp:      time.Now(),
		PolicyName:     s.config.DefaultPolicy,
	}

	// Store media
	ref, err := s.config.StorageService.StoreMedia(ctx, media, metadata)
	if err != nil {
		return fmt.Errorf("failed to store media: %w", err)
	}

	// Update media content to reference storage
	refStr := string(ref)
	media.StorageReference = &refStr

	// Clear inline data to save memory
	media.Data = nil

	// Set size if not already set
	if media.SizeKB == nil && metadata.SizeBytes > 0 {
		sizeKB := metadata.SizeBytes / 1024
		media.SizeKB = &sizeKB
	}

	logger.Debug("Externalized media", "message_idx", messageIdx, "part_idx", partIdx, "ref", refStr)

	return nil
}

// TruncationStrategy defines how to handle messages when over token budget.
type TruncationStrategy string

const (
	// TruncateOldest drops oldest messages first
	TruncateOldest TruncationStrategy = "oldest"
	// TruncateLeastRelevant drops least relevant messages (requires embeddings)
	TruncateLeastRelevant TruncationStrategy = "relevance"
	// TruncateSummarize compresses old messages into summaries
	TruncateSummarize TruncationStrategy = "summarize"
	// TruncateFail returns error if over budget
	TruncateFail TruncationStrategy = "fail"
)

// QuerySourceType defines how to construct the relevance query.
type QuerySourceType string

const (
	// QuerySourceLastUser uses the last user message as the query
	QuerySourceLastUser QuerySourceType = "last_user"
	// QuerySourceLastN concatenates the last N messages as the query
	QuerySourceLastN QuerySourceType = "last_n"
	// QuerySourceCustom uses a custom query string
	QuerySourceCustom QuerySourceType = "custom"
)

// RelevanceConfig configures embedding-based relevance truncation.
// Used when TruncationStrategy is TruncateLeastRelevant.
type RelevanceConfig struct {
	// EmbeddingProvider generates embeddings for similarity scoring.
	// Required for relevance-based truncation; if nil, falls back to oldest.
	EmbeddingProvider providers.EmbeddingProvider

	// MinRecentMessages always keeps the N most recent messages
	// regardless of relevance score. Default: 3
	MinRecentMessages int

	// AlwaysKeepSystemRole keeps all system role messages regardless of score.
	AlwaysKeepSystemRole bool

	// SimilarityThreshold is the minimum score to consider a message relevant (0.0-1.0).
	// Messages below this threshold may be dropped first.
	SimilarityThreshold float64

	// QuerySource determines what text to compare messages against.
	// Default: QuerySourceLastUser
	QuerySource QuerySourceType

	// LastNCount is the number of messages to use when QuerySource is QuerySourceLastN.
	// Default: 3
	LastNCount int

	// CustomQuery is the query text when QuerySource is QuerySourceCustom.
	CustomQuery string

	// CacheEmbeddings enables caching of embeddings across truncation calls.
	// Useful when context changes incrementally.
	CacheEmbeddings bool
}

// ContextBuilderPolicy defines token budget and truncation behavior.
type ContextBuilderPolicy struct {
	TokenBudget      int
	ReserveForOutput int
	Strategy         TruncationStrategy
	CacheBreakpoints bool

	// RelevanceConfig for TruncateLeastRelevant strategy (optional).
	// If nil when using TruncateLeastRelevant, falls back to TruncateOldest.
	RelevanceConfig *RelevanceConfig
}

// ContextBuilderStage manages token budget and truncates messages if needed.
//
// This stage ensures the conversation context fits within the LLM's token budget
// by applying truncation strategies when messages exceed the limit.
//
// Token budget calculation:
//
//	available = TokenBudget - ReserveForOutput - systemPromptTokens
//
// Truncation strategies (TruncationStrategy):
//   - TruncateOldest: removes oldest messages first (keeps most recent context)
//   - TruncateLeastRelevant: removes least relevant messages (requires embeddings) [TODO]
//   - TruncateSummarize: compresses old messages into summaries [TODO]
//   - TruncateFail: returns error if budget exceeded (strict mode)
//
// Configuration (ContextBuilderPolicy):
//   - TokenBudget: total tokens allowed (0 = unlimited, pass-through mode)
//   - ReserveForOutput: tokens reserved for LLM response
//   - Strategy: truncation strategy to apply
//   - CacheBreakpoints: enable prompt caching hints
//
// Metadata added:
//   - context_truncated: true if truncation was applied
//   - enable_cache_breakpoints: copied from policy.CacheBreakpoints
//
// This is an Accumulate stage: N input elements → N (possibly fewer) output elements
type ContextBuilderStage struct {
	BaseStage
	policy *ContextBuilderPolicy
}

// NewContextBuilderStage creates a context builder stage.
func NewContextBuilderStage(policy *ContextBuilderPolicy) *ContextBuilderStage {
	return &ContextBuilderStage{
		BaseStage: NewBaseStage("context_builder", StageTypeAccumulate),
		policy:    policy,
	}
}

// Process enforces token budget and truncates messages if needed.
func (s *ContextBuilderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	// No policy or unlimited budget - pass through
	if s.policy == nil || s.policy.TokenBudget <= 0 {
		for elem := range input {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	// Accumulate all messages
	var messages []types.Message
	var systemPrompt string
	var firstElem *StreamElement

	for elem := range input {
		if firstElem == nil {
			firstElem = &elem
		}

		if elem.Message != nil {
			messages = append(messages, *elem.Message)
		}

		// Extract system prompt from metadata
		if sp, ok := elem.Metadata["system_prompt"].(string); ok {
			systemPrompt = sp
		}
	}

	// Calculate available budget
	available := s.policy.TokenBudget - s.policy.ReserveForOutput
	systemTokens := s.countTokens(systemPrompt)
	available -= systemTokens

	if available <= 0 {
		return fmt.Errorf("token budget too small: need at least %d for system prompt", systemTokens)
	}

	// Calculate current token usage
	currentTokens := s.countMessagesTokens(messages)

	// If under budget, emit all messages
	if currentTokens <= available {
		logger.Debug("Context under budget", "current", currentTokens, "available", available)
		return s.emitMessages(ctx, messages, firstElem, output, false)
	}

	// Apply truncation strategy
	truncated, err := s.truncateMessages(ctx, messages, available)
	if err != nil {
		return fmt.Errorf("context builder: %w", err)
	}

	logger.Warn("Context truncated", "original", len(messages), "truncated", len(truncated), "strategy", s.policy.Strategy)

	// Emit truncated messages with metadata
	return s.emitMessages(ctx, truncated, firstElem, output, true)
}

// emitMessages emits accumulated messages as elements.
func (s *ContextBuilderStage) emitMessages(
	ctx context.Context,
	messages []types.Message,
	template *StreamElement,
	output chan<- StreamElement,
	truncated bool,
) error {
	for i := range messages {
		elem := StreamElement{
			Message:  &messages[i],
			Metadata: make(map[string]interface{}),
		}

		// Copy metadata from template
		if template != nil && template.Metadata != nil {
			for k, v := range template.Metadata {
				elem.Metadata[k] = v
			}
		}

		// Add truncation info
		if truncated {
			elem.Metadata["context_truncated"] = true
		}

		// Add cache breakpoint flag if enabled
		if s.policy.CacheBreakpoints {
			elem.Metadata["enable_cache_breakpoints"] = true
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// countTokens estimates token count using a simple heuristic.
func (s *ContextBuilderStage) countTokens(text string) int {
	if text == "" {
		return 0
	}
	words := strings.Fields(text)
	return int(float64(len(words)) * 1.3)
}

// countMessagesTokens estimates total tokens for messages.
func (s *ContextBuilderStage) countMessagesTokens(messages []types.Message) int {
	total := 0
	for i := range messages {
		total += s.countTokens(messages[i].Content)
		for _, tc := range messages[i].ToolCalls {
			total += s.countTokens(string(tc.Args))
		}
	}
	return total
}

// truncateMessages applies truncation strategy.
func (s *ContextBuilderStage) truncateMessages(
	ctx context.Context,
	messages []types.Message,
	budget int,
) ([]types.Message, error) {
	switch s.policy.Strategy {
	case TruncateOldest:
		return s.truncateOldest(messages, budget), nil
	case TruncateLeastRelevant:
		return s.truncateByRelevance(ctx, messages, budget)
	case TruncateSummarize:
		// TODO: Implement LLM-based summarization
		logger.Warn("Summarization truncation not implemented, falling back to oldest strategy")
		return s.truncateOldest(messages, budget), nil
	case TruncateFail:
		return nil, fmt.Errorf("token budget exceeded: have %d, budget %d", s.countMessagesTokens(messages), budget)
	default:
		return s.truncateOldest(messages, budget), nil
	}
}

// truncateOldest keeps most recent messages that fit budget.
func (s *ContextBuilderStage) truncateOldest(messages []types.Message, budget int) []types.Message {
	var result []types.Message
	used := 0

	// Start from most recent, work backwards
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		msgTokens := s.countTokens(msg.Content)

		// Add tool call tokens
		for _, tc := range msg.ToolCalls {
			msgTokens += s.countTokens(string(tc.Args))
		}

		if used+msgTokens > budget {
			break
		}

		result = append([]types.Message{msg}, result...) // Prepend
		used += msgTokens
	}

	return result
}

// truncateByRelevance removes least relevant messages based on embedding similarity.
// Falls back to truncateOldest if EmbeddingProvider is not configured.
func (s *ContextBuilderStage) truncateByRelevance(
	ctx context.Context,
	messages []types.Message,
	budget int,
) ([]types.Message, error) {
	cfg := s.policy.RelevanceConfig

	// Fall back to oldest if no embedding provider
	if cfg == nil || cfg.EmbeddingProvider == nil {
		logger.Warn("No embedding provider configured, falling back to oldest strategy")
		return s.truncateOldest(messages, budget), nil
	}

	// Default configuration values
	minRecent := cfg.MinRecentMessages
	if minRecent <= 0 {
		minRecent = 3
	}

	// Build the query text based on QuerySource
	queryText := s.buildRelevanceQuery(messages, cfg)
	if queryText == "" {
		logger.Warn("Empty relevance query, falling back to oldest strategy")
		return s.truncateOldest(messages, budget), nil
	}

	// Build scored messages with token counts and protection flags
	scored := s.buildScoredMessages(messages, minRecent, cfg.AlwaysKeepSystemRole)

	// Get embeddings for all message texts plus the query
	texts := make([]string, 0, len(messages)+1)
	texts = append(texts, queryText) // Query at index 0
	for i := range scored {
		texts = append(texts, s.extractMessageText(&scored[i].Message))
	}

	// Generate embeddings
	resp, err := cfg.EmbeddingProvider.Embed(ctx, providers.EmbeddingRequest{Texts: texts})
	if err != nil {
		logger.Error("Embedding generation failed, falling back to oldest", "error", err)
		return s.truncateOldest(messages, budget), nil
	}

	if len(resp.Embeddings) != len(texts) {
		logger.Error("Embedding count mismatch, falling back to oldest",
			"expected", len(texts), "got", len(resp.Embeddings))
		return s.truncateOldest(messages, budget), nil
	}
	embeddings := resp.Embeddings

	// Compute similarity scores (query embedding is at index 0)
	queryEmbedding := embeddings[0]
	for i := range scored {
		messageEmbedding := embeddings[i+1] // +1 to skip query
		scored[i].Score = CosineSimilarity(queryEmbedding, messageEmbedding)
	}

	// Select messages to keep based on relevance and budget
	return s.selectByRelevance(scored, budget, cfg.SimilarityThreshold), nil
}

// buildRelevanceQuery constructs the query text for relevance comparison.
func (s *ContextBuilderStage) buildRelevanceQuery(
	messages []types.Message,
	cfg *RelevanceConfig,
) string {
	switch cfg.QuerySource {
	case QuerySourceCustom:
		return cfg.CustomQuery

	case QuerySourceLastN:
		lastN := cfg.LastNCount
		if lastN <= 0 {
			lastN = 3
		}
		start := len(messages) - lastN
		if start < 0 {
			start = 0
		}
		var parts []string
		for i := start; i < len(messages); i++ {
			parts = append(parts, s.extractMessageText(&messages[i]))
		}
		return strings.Join(parts, " ")

	case QuerySourceLastUser:
		// Use last user message as query
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				return s.extractMessageText(&messages[i])
			}
		}
		// No user message found, use last message
		if len(messages) > 0 {
			return s.extractMessageText(&messages[len(messages)-1])
		}
	}
	return ""
}

// buildScoredMessages creates ScoredMessage entries with protection flags.
func (s *ContextBuilderStage) buildScoredMessages(
	messages []types.Message,
	minRecent int,
	keepSystemRole bool,
) []ScoredMessage {
	scored := make([]ScoredMessage, len(messages))

	for i := range messages {
		scored[i] = ScoredMessage{
			Index:      i,
			Message:    messages[i],
			TokenCount: s.countMessageTokens(&messages[i]),
		}

		// Mark as protected if system role and keepSystemRole is true
		if keepSystemRole && messages[i].Role == "system" {
			scored[i].IsProtected = true
		}

		// Mark most recent messages as protected
		if i >= len(messages)-minRecent {
			scored[i].IsProtected = true
		}
	}

	return scored
}

// countMessageTokens counts tokens for a single message.
func (s *ContextBuilderStage) countMessageTokens(msg *types.Message) int {
	tokens := s.countTokens(msg.Content)
	for _, tc := range msg.ToolCalls {
		tokens += s.countTokens(string(tc.Args))
	}
	return tokens
}

// extractMessageText gets the text content from a message for embedding.
func (s *ContextBuilderStage) extractMessageText(msg *types.Message) string {
	if msg.Content != "" {
		return msg.Content
	}

	// Try parts if Content is empty
	var parts []string
	for _, part := range msg.Parts {
		if part.Text != nil && *part.Text != "" {
			parts = append(parts, *part.Text)
		}
	}
	return strings.Join(parts, " ")
}

// selectByRelevance selects messages to keep based on relevance scores and budget.
// Protected messages are always kept. Remaining budget is filled by highest-scoring messages.
func (s *ContextBuilderStage) selectByRelevance(
	scored []ScoredMessage,
	budget int,
	threshold float64,
) []types.Message {
	protected, candidates := s.separateProtectedMessages(scored)
	protectedTokens := s.sumTokens(protected)

	// If protected messages already exceed budget, fall back to keeping most recent
	if protectedTokens > budget {
		return s.selectMostRecentProtected(protected, budget)
	}

	// Select candidates by score within remaining budget
	remainingBudget := budget - protectedTokens
	selected := s.selectCandidatesByScore(candidates, remainingBudget, threshold)

	// Combine and sort by original index
	return s.combineAndSort(protected, selected)
}

// separateProtectedMessages splits scored messages into protected and candidate slices.
func (s *ContextBuilderStage) separateProtectedMessages(
	scored []ScoredMessage,
) (protected, candidates []ScoredMessage) {
	for i := range scored {
		if scored[i].IsProtected {
			protected = append(protected, scored[i])
		} else {
			candidates = append(candidates, scored[i])
		}
	}
	return protected, candidates
}

// sumTokens calculates total tokens for a slice of scored messages.
func (s *ContextBuilderStage) sumTokens(messages []ScoredMessage) int {
	total := 0
	for i := range messages {
		total += messages[i].TokenCount
	}
	return total
}

// selectMostRecentProtected handles edge case where protected messages exceed budget.
func (s *ContextBuilderStage) selectMostRecentProtected(
	protected []ScoredMessage,
	budget int,
) []types.Message {
	logger.Warn("Protected messages exceed budget, keeping most recent only")

	// Sort by index descending (most recent first)
	sort.Sort(sort.Reverse(ByOriginalIndex(protected)))

	var result []types.Message
	used := 0
	for i := range protected {
		if used+protected[i].TokenCount <= budget {
			result = append(result, protected[i].Message)
			used += protected[i].TokenCount
		}
	}
	return result
}

// selectCandidatesByScore selects highest-scoring candidates within budget.
func (s *ContextBuilderStage) selectCandidatesByScore(
	candidates []ScoredMessage,
	budget int,
	threshold float64,
) []ScoredMessage {
	sort.Sort(ScoredMessages(candidates))

	var selected []ScoredMessage
	usedTokens := 0
	for i := range candidates {
		if threshold > 0 && candidates[i].Score < threshold {
			continue
		}
		if usedTokens+candidates[i].TokenCount <= budget {
			selected = append(selected, candidates[i])
			usedTokens += candidates[i].TokenCount
		}
	}
	return selected
}

// combineAndSort merges protected and selected messages, sorts by original index.
func (s *ContextBuilderStage) combineAndSort(
	protected, selected []ScoredMessage,
) []types.Message {
	allSelected := make([]ScoredMessage, 0, len(protected)+len(selected))
	allSelected = append(allSelected, protected...)
	allSelected = append(allSelected, selected...)
	sort.Sort(ByOriginalIndex(allSelected))

	result := make([]types.Message, len(allSelected))
	for i := range allSelected {
		result[i] = allSelected[i].Message
	}

	logger.Debug("Relevance truncation complete",
		"protected", len(protected),
		"selected", len(selected),
		"total", len(result),
	)
	return result
}
