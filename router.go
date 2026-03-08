package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	openai "github.com/sashabaranov/go-openai"

	"github.com/arman/docint/internal/config"
	"github.com/arman/docint/internal/domain"
)

// Router selects the appropriate model based on extraction complexity
// and handles retries with model escalation.
type Router struct {
	client              *openai.Client
	fastModel           string
	strongModel         string
	maxRetries          int
	complexityThreshold int
}

func NewRouter(cfg config.LLMConfig) *Router {
	client := openai.NewClient(cfg.OpenAIKey)
	return &Router{
		client:              client,
		fastModel:           cfg.FastModel,
		strongModel:         cfg.StrongModel,
		maxRetries:          cfg.MaxRetries,
		complexityThreshold: cfg.ComplexityThreshold,
	}
}

// ExtractionRequest represents a single chunk + schema to extract against.
type ExtractionRequest struct {
	ChunkText string
	Schema    json.RawMessage
	ChunkIndex int
	TotalChunks int
}

// ExtractionResponse is the structured result from one LLM call.
type ExtractionResponse struct {
	Fields      map[string]interface{} `json:"fields"`
	RawJSON     json.RawMessage
	ModelUsed   domain.ModelTier
	TokensIn    int
	TokensOut   int
}

// Extract performs extraction on a single chunk, with model routing and retry logic.
func (r *Router) Extract(ctx context.Context, req ExtractionRequest) (*ExtractionResponse, error) {
	// Determine initial model tier based on complexity signals
	tier := r.selectTier(req)
	model := r.modelForTier(tier)

	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		resp, err := r.callLLM(ctx, model, req)
		if err != nil {
			lastErr = err
			log.Warn().
				Err(err).
				Str("model", model).
				Int("attempt", attempt).
				Msg("LLM call failed")

			// On failure, escalate to strong model if we haven't already
			if tier == domain.ModelTierFast {
				tier = domain.ModelTierStrong
				model = r.modelForTier(tier)
				log.Info().Str("model", model).Msg("escalating to strong model")
			}
			continue
		}

		// Validate the response is valid JSON matching our expectations
		if err := r.validateResponse(resp.RawJSON, req.Schema); err != nil {
			log.Warn().
				Err(err).
				Str("model", model).
				Int("attempt", attempt).
				Msg("response validation failed")

			// Escalate on validation failure
			if tier == domain.ModelTierFast {
				tier = domain.ModelTierStrong
				model = r.modelForTier(tier)
			}
			lastErr = err
			continue
		}

		resp.ModelUsed = tier
		return resp, nil
	}

	return nil, fmt.Errorf("extraction failed after %d attempts: %w", r.maxRetries+1, lastErr)
}

// selectTier determines which model to use based on complexity heuristics.
func (r *Router) selectTier(req ExtractionRequest) domain.ModelTier {
	// Heuristic 1: Schema field count
	fieldCount := countSchemaFields(req.Schema)
	if fieldCount > r.complexityThreshold {
		return domain.ModelTierStrong
	}

	// Heuristic 2: Chunk length (longer text = more complex extraction)
	if len(req.ChunkText) > 3000 {
		return domain.ModelTierStrong
	}

	// Heuristic 3: Nested arrays/objects in schema suggest complex extraction
	if hasNestedStructures(req.Schema) {
		return domain.ModelTierStrong
	}

	return domain.ModelTierFast
}

func (r *Router) modelForTier(tier domain.ModelTier) string {
	if tier == domain.ModelTierStrong {
		return r.strongModel
	}
	return r.fastModel
}

func (r *Router) callLLM(ctx context.Context, model string, req ExtractionRequest) (*ExtractionResponse, error) {
	systemPrompt := buildSystemPrompt(req.Schema, req.ChunkIndex, req.TotalChunks)
	userPrompt := buildUserPrompt(req.ChunkText)

	chatReq := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.0, // deterministic extraction
	}

	chatResp, err := r.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("OpenAI API call failed: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from model")
	}

	rawJSON := json.RawMessage(chatResp.Choices[0].Message.Content)

	var fields map[string]interface{}
	if err := json.Unmarshal(rawJSON, &fields); err != nil {
		return nil, fmt.Errorf("response is not valid JSON: %w", err)
	}

	return &ExtractionResponse{
		Fields:    fields,
		RawJSON:   rawJSON,
		TokensIn:  chatResp.Usage.PromptTokens,
		TokensOut: chatResp.Usage.CompletionTokens,
	}, nil
}

// validateResponse checks that the LLM output contains the expected structure.
func (r *Router) validateResponse(raw json.RawMessage, schema json.RawMessage) error {
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("response is not a JSON object: %w", err)
	}

	// Parse schema to get required fields
	var schemaDef map[string]interface{}
	if err := json.Unmarshal(schema, &schemaDef); err != nil {
		return nil // can't validate without a parseable schema
	}

	required, _ := schemaDef["required"].([]interface{})
	for _, r := range required {
		fieldName, ok := r.(string)
		if !ok {
			continue
		}
		if _, exists := result[fieldName]; !exists {
			return fmt.Errorf("required field %q missing from response", fieldName)
		}
	}

	return nil
}

// --- Prompt Construction ---

func buildSystemPrompt(schema json.RawMessage, chunkIdx, totalChunks int) string {
	var b strings.Builder

	b.WriteString("You are a document data extraction engine. Your task is to extract structured data from the provided document text according to the specified schema.\n\n")
	b.WriteString("RULES:\n")
	b.WriteString("1. Return ONLY a JSON object matching the schema below.\n")
	b.WriteString("2. If a field's value is not found in the text, set it to null.\n")
	b.WriteString("3. Do not invent or hallucinate values. Only extract what is explicitly stated.\n")
	b.WriteString("4. For arrays, include all matching items found in the text.\n")
	b.WriteString("5. Maintain exact values — do not paraphrase names, numbers, or dates.\n\n")

	if totalChunks > 1 {
		b.WriteString(fmt.Sprintf("NOTE: This is chunk %d of %d from a larger document. Extract whatever matching data appears in this chunk. Fields not present in this chunk should be null.\n\n", chunkIdx+1, totalChunks))
	}

	b.WriteString("EXTRACTION SCHEMA:\n")
	b.WriteString(string(schema))
	b.WriteString("\n")

	return b.String()
}

func buildUserPrompt(text string) string {
	return fmt.Sprintf("DOCUMENT TEXT:\n\n%s", text)
}

// --- Schema Analysis Helpers ---

func countSchemaFields(schema json.RawMessage) int {
	var s map[string]interface{}
	if err := json.Unmarshal(schema, &s); err != nil {
		return 0
	}
	props, ok := s["properties"].(map[string]interface{})
	if !ok {
		return 0
	}
	return len(props)
}

func hasNestedStructures(schema json.RawMessage) bool {
	var s map[string]interface{}
	if err := json.Unmarshal(schema, &s); err != nil {
		return false
	}
	props, ok := s["properties"].(map[string]interface{})
	if !ok {
		return false
	}
	for _, v := range props {
		prop, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		t, _ := prop["type"].(string)
		if t == "array" || t == "object" {
			return true
		}
	}
	return false
}
