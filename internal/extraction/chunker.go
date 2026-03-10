package extraction

import (
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/agxp/docpulse/internal/domain"
)

// ChunkConfig controls chunking behavior.
type ChunkConfig struct {
	MaxChunkSize int // max characters per chunk (not tokens — approximation is fine)
	OverlapSize  int // characters of overlap between consecutive chunks
	MaxChunks    int // hard limit on chunk count per document
}

func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{
		MaxChunkSize: 4000, // ~1000 tokens, leaves room for schema + system prompt
		OverlapSize:  200,  // enough to preserve context at boundaries
		MaxChunks:    200,
	}
}

// Chunker splits extracted text into overlapping chunks that respect
// semantic boundaries (paragraphs, sentences) rather than cutting mid-word.
type Chunker struct {
	config ChunkConfig
}

func NewChunker(cfg ChunkConfig) *Chunker {
	return &Chunker{config: cfg}
}

// Chunk splits text into a sequence of Chunk objects ready for LLM processing.
// Strategy: split on paragraph boundaries first, then sentence boundaries,
// with configurable overlap between consecutive chunks.
func (c *Chunker) Chunk(jobID uuid.UUID, text string) []domain.Chunk {
	if len(text) <= c.config.MaxChunkSize {
		// Document fits in a single chunk — no splitting needed
		return []domain.Chunk{{
			ID:       uuid.New(),
			JobID:    jobID,
			Sequence: 0,
			Content:  text,
			Status:   domain.JobStatusPending,
		}}
	}

	paragraphs := splitParagraphs(text)
	var chunks []domain.Chunk
	var current strings.Builder
	seq := 0

	for _, para := range paragraphs {
		// If adding this paragraph would exceed the limit, finalize current chunk
		if current.Len() > 0 && current.Len()+len(para)+1 > c.config.MaxChunkSize {
			chunk := c.finalizeChunk(jobID, seq, current.String())
			chunks = append(chunks, chunk)
			seq++

			if len(chunks) >= c.config.MaxChunks {
				break
			}

			// Start new chunk with overlap from the end of the previous chunk
			overlap := c.getOverlap(current.String())
			current.Reset()
			current.WriteString(overlap)
		}

		// If a single paragraph exceeds the max size, split it by sentences
		if len(para) > c.config.MaxChunkSize {
			if current.Len() > 0 {
				chunk := c.finalizeChunk(jobID, seq, current.String())
				chunks = append(chunks, chunk)
				seq++
				current.Reset()
			}

			sentenceChunks := c.splitLongParagraph(jobID, &seq, para)
			chunks = append(chunks, sentenceChunks...)
			continue
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}

	// Don't forget the last chunk (respect MaxChunks cap)
	if current.Len() > 0 && len(chunks) < c.config.MaxChunks {
		chunk := c.finalizeChunk(jobID, seq, current.String())
		chunks = append(chunks, chunk)
	}

	return chunks
}

func (c *Chunker) finalizeChunk(jobID uuid.UUID, seq int, content string) domain.Chunk {
	return domain.Chunk{
		ID:       uuid.New(),
		JobID:    jobID,
		Sequence: seq,
		Content:  strings.TrimSpace(content),
		Status:   domain.JobStatusPending,
	}
}

// getOverlap returns the last N characters of text, breaking at a sentence boundary.
func (c *Chunker) getOverlap(text string) string {
	if len(text) <= c.config.OverlapSize {
		return text
	}

	overlap := text[len(text)-c.config.OverlapSize:]

	// Try to start at a sentence boundary
	sentenceStarts := []string{". ", "! ", "? ", ".\n", "!\n", "?\n"}
	bestIdx := -1
	for _, sep := range sentenceStarts {
		if idx := strings.Index(overlap, sep); idx != -1 {
			candidate := idx + len(sep)
			if candidate > bestIdx {
				bestIdx = candidate
			}
		}
	}

	if bestIdx > 0 && bestIdx < len(overlap) {
		return overlap[bestIdx:]
	}

	// Fallback: start at a word boundary
	for i, r := range overlap {
		if unicode.IsSpace(r) {
			return overlap[i+1:]
		}
	}

	return overlap
}

// splitLongParagraph breaks a paragraph that exceeds max chunk size into sentence-level chunks.
func (c *Chunker) splitLongParagraph(jobID uuid.UUID, seq *int, para string) []domain.Chunk {
	sentences := splitSentences(para)
	var chunks []domain.Chunk
	var current strings.Builder

	for _, sent := range sentences {
		if current.Len() > 0 && current.Len()+len(sent)+1 > c.config.MaxChunkSize {
			chunk := c.finalizeChunk(jobID, *seq, current.String())
			chunks = append(chunks, chunk)
			*seq++

			overlap := c.getOverlap(current.String())
			current.Reset()
			current.WriteString(overlap)
		}

		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sent)
	}

	if current.Len() > 0 {
		chunk := c.finalizeChunk(jobID, *seq, current.String())
		chunks = append(chunks, chunk)
		*seq++
	}

	return chunks
}

// --- Text splitting utilities ---

func splitParagraphs(text string) []string {
	// Split on double newlines (standard paragraph separator)
	raw := strings.Split(text, "\n\n")
	var paragraphs []string
	for _, p := range raw {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}
	return paragraphs
}

func splitSentences(text string) []string {
	// Simple sentence splitter — split on . ! ? followed by space or newline.
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) || unicode.IsUpper(runes[i+1]) {
				sentence := strings.TrimSpace(current.String())
				if sentence != "" {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	if remaining := strings.TrimSpace(current.String()); remaining != "" {
		sentences = append(sentences, remaining)
	}

	return sentences
}
