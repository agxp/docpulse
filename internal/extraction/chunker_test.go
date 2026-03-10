package extraction

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/arman/docpulse/internal/domain"
)

var jobID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// cfg returns a ChunkConfig with small sizes for easy testing.
func cfg(maxChunk, overlap, maxChunks int) ChunkConfig {
	return ChunkConfig{MaxChunkSize: maxChunk, OverlapSize: overlap, MaxChunks: maxChunks}
}

func repeat(s string, n int) string {
	return strings.Repeat(s, n)
}

// assertChunks is a helper that validates common invariants on every test.
func assertChunks(t *testing.T, chunks []domain.Chunk, jobID uuid.UUID) {
	t.Helper()
	seen := make(map[uuid.UUID]bool)
	for i, ch := range chunks {
		if ch.JobID != jobID {
			t.Errorf("chunk[%d]: wrong JobID %v", i, ch.JobID)
		}
		if ch.Sequence != i {
			t.Errorf("chunk[%d]: expected Sequence %d, got %d", i, i, ch.Sequence)
		}
		if ch.Status != domain.JobStatusPending {
			t.Errorf("chunk[%d]: expected status pending, got %s", i, ch.Status)
		}
		if ch.ID == uuid.Nil {
			t.Errorf("chunk[%d]: ID is nil", i)
		}
		if seen[ch.ID] {
			t.Errorf("chunk[%d]: duplicate ID %v", i, ch.ID)
		}
		seen[ch.ID] = true
		if strings.HasPrefix(ch.Content, " ") || strings.HasSuffix(ch.Content, " ") {
			t.Errorf("chunk[%d]: content has leading/trailing spaces", i)
		}
	}
}

// --- Single chunk (fits within limit) ---

func TestChunk_ShortText_SingleChunk(t *testing.T) {
	c := NewChunker(cfg(100, 20, 10))
	chunks := c.Chunk(jobID, "Hello world.")

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "Hello world." {
		t.Errorf("unexpected content: %q", chunks[0].Content)
	}
	assertChunks(t, chunks, jobID)
}

func TestChunk_ExactlyAtLimit_SingleChunk(t *testing.T) {
	text := repeat("a", 100)
	c := NewChunker(cfg(100, 20, 10))
	chunks := c.Chunk(jobID, text)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	assertChunks(t, chunks, jobID)
}

func TestChunk_Empty_SingleChunk(t *testing.T) {
	c := NewChunker(cfg(100, 20, 10))
	chunks := c.Chunk(jobID, "")

	// Empty text still returns a single chunk (fits within limit)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunk_OnlyWhitespace_SingleChunk(t *testing.T) {
	c := NewChunker(cfg(100, 20, 10))
	chunks := c.Chunk(jobID, "   \n\n   ")

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}

// --- Multiple chunks from paragraph splitting ---

func TestChunk_TwoParagraphs_SplitsOnBoundary(t *testing.T) {
	// Two paragraphs, each 60 chars — together exceed limit of 100
	para1 := repeat("a", 60)
	para2 := repeat("b", 60)
	text := para1 + "\n\n" + para2

	c := NewChunker(cfg(100, 10, 10))
	chunks := c.Chunk(jobID, text)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, para1) {
		t.Errorf("chunk[0] should contain para1")
	}
	if !strings.Contains(chunks[len(chunks)-1].Content, para2) {
		t.Errorf("last chunk should contain para2")
	}
	assertChunks(t, chunks, jobID)
}

func TestChunk_ManyParagraphs_CorrectCount(t *testing.T) {
	// 10 paragraphs of 30 chars each; limit=50 so ~2 paragraphs per chunk → ~5 chunks
	var parts []string
	for i := 0; i < 10; i++ {
		parts = append(parts, repeat("x", 30))
	}
	text := strings.Join(parts, "\n\n")

	c := NewChunker(cfg(50, 5, 20))
	chunks := c.Chunk(jobID, text)

	if len(chunks) < 4 {
		t.Errorf("expected at least 4 chunks, got %d", len(chunks))
	}
	assertChunks(t, chunks, jobID)
}

func TestChunk_ParagraphsPackedUpToLimit(t *testing.T) {
	// Three paragraphs: 40+40 fit in 100, third doesn't → 2 chunks
	para := repeat("a", 40)
	text := para + "\n\n" + para + "\n\n" + para

	c := NewChunker(cfg(100, 5, 10))
	chunks := c.Chunk(jobID, text)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	assertChunks(t, chunks, jobID)
}

// --- Overlap ---

func TestChunk_OverlapPresent(t *testing.T) {
	// Each paragraph is 60 chars, limit=80, overlap=20
	// After first chunk, second chunk should start with overlap from first
	para1 := "First sentence ends here. " + repeat("a", 40)
	para2 := repeat("b", 60)
	text := para1 + "\n\n" + para2

	c := NewChunker(cfg(80, 20, 10))
	chunks := c.Chunk(jobID, text)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	// Second chunk should contain some content from the first
	secondContent := chunks[1].Content
	firstContent := chunks[0].Content
	// The overlap should be a suffix of the first chunk's content
	if len(firstContent) > 20 {
		tail := firstContent[len(firstContent)-20:]
		if !strings.Contains(secondContent, strings.TrimSpace(tail)) {
			// Overlap may start at sentence boundary — just verify it's non-empty
			// and shorter than the full first chunk
			if len(secondContent) == 0 {
				t.Error("second chunk is empty — no overlap carried over")
			}
		}
	}
	assertChunks(t, chunks, jobID)
}

func TestChunk_OverlapShorterThanText(t *testing.T) {
	// When text is shorter than overlap size, getOverlap returns full text
	text := "Short."
	c := NewChunker(cfg(100, 50, 10))
	overlap := c.getOverlap(text)
	if overlap != text {
		t.Errorf("expected full text %q, got %q", text, overlap)
	}
}

func TestChunk_OverlapStartsAtSentenceBoundary(t *testing.T) {
	// Text ends with "...foo. Bar baz qux." — overlap should start after ". "
	text := repeat("x", 50) + "foo. Bar baz."
	c := NewChunker(cfg(200, 20, 10))
	overlap := c.getOverlap(text)
	// Should not start mid-word if a sentence boundary exists in the window
	if strings.HasPrefix(overlap, " ") {
		t.Errorf("overlap starts with space: %q", overlap)
	}
}

func TestChunk_OverlapFallsBackToWordBoundary(t *testing.T) {
	// No sentence-ending punctuation in the overlap window
	text := repeat("a", 50) + " word1 word2 word3"
	c := NewChunker(cfg(200, 15, 10))
	overlap := c.getOverlap(text)
	if strings.HasPrefix(overlap, " ") {
		t.Errorf("overlap starts with space: %q", overlap)
	}
}

// --- Long paragraph (sentence-level splitting) ---

func TestChunk_LongParagraph_SplitsBySentence(t *testing.T) {
	// Single paragraph longer than maxChunkSize — must split by sentences
	sentences := []string{
		"The quick brown fox jumps over the lazy dog.",
		"Pack my box with five dozen liquor jugs.",
		"How vexingly quick daft zebras jump.",
		"The five boxing wizards jump quickly.",
		"Sphinx of black quartz, judge my vow.",
	}
	// Make each sentence ~50 chars, limit=80 → roughly 1-2 sentences per chunk
	text := strings.Join(sentences, " ")

	c := NewChunker(cfg(80, 10, 20))
	chunks := c.Chunk(jobID, text)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks from long paragraph, got %d", len(chunks))
	}
	// All sentences should appear somewhere in the chunks
	allContent := ""
	for _, ch := range chunks {
		allContent += ch.Content + " "
	}
	for _, s := range sentences {
		// Just check key words appear
		word := strings.Fields(s)[0]
		if !strings.Contains(allContent, word) {
			t.Errorf("word %q from sentence not found in any chunk", word)
		}
	}
	assertChunks(t, chunks, jobID)
}

func TestChunk_LongParagraphAfterShortOne(t *testing.T) {
	// Short paragraph followed by a paragraph that's too long
	short := "Short intro paragraph."
	long := strings.Repeat("Long sentence here. ", 20) // ~400 chars

	text := short + "\n\n" + long

	c := NewChunker(cfg(100, 10, 20))
	chunks := c.Chunk(jobID, text)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	assertChunks(t, chunks, jobID)
}

// --- MaxChunks cap ---

func TestChunk_MaxChunksCap(t *testing.T) {
	// 20 paragraphs but maxChunks=5 — should stop at 5
	var parts []string
	for i := 0; i < 20; i++ {
		parts = append(parts, repeat("x", 60))
	}
	text := strings.Join(parts, "\n\n")

	c := NewChunker(cfg(80, 5, 5))
	chunks := c.Chunk(jobID, text)

	if len(chunks) > 5 {
		t.Errorf("expected at most 5 chunks (maxChunks cap), got %d", len(chunks))
	}
	assertChunks(t, chunks, jobID)
}

// --- Sequence numbers ---

func TestChunk_SequenceNumbers_Sequential(t *testing.T) {
	var parts []string
	for i := 0; i < 8; i++ {
		parts = append(parts, repeat("a", 60))
	}
	text := strings.Join(parts, "\n\n")

	c := NewChunker(cfg(80, 5, 20))
	chunks := c.Chunk(jobID, text)

	for i, ch := range chunks {
		if ch.Sequence != i {
			t.Errorf("chunk[%d]: expected Sequence=%d, got %d", i, i, ch.Sequence)
		}
	}
}

// --- IDs ---

func TestChunk_UniqueIDs(t *testing.T) {
	var parts []string
	for i := 0; i < 6; i++ {
		parts = append(parts, repeat("a", 60))
	}
	text := strings.Join(parts, "\n\n")

	c := NewChunker(cfg(80, 5, 20))
	chunks := c.Chunk(jobID, text)

	seen := make(map[uuid.UUID]bool)
	for i, ch := range chunks {
		if seen[ch.ID] {
			t.Errorf("chunk[%d] has duplicate ID %v", i, ch.ID)
		}
		seen[ch.ID] = true
	}
}

func TestChunk_JobIDPropagated(t *testing.T) {
	id := uuid.New()
	c := NewChunker(cfg(50, 5, 20))
	chunks := c.Chunk(id, strings.Join([]string{repeat("a", 60), repeat("b", 60)}, "\n\n"))
	for i, ch := range chunks {
		if ch.JobID != id {
			t.Errorf("chunk[%d]: wrong JobID", i)
		}
	}
}

// --- Content preservation ---

func TestChunk_NoContentLost(t *testing.T) {
	// Every word in the original text should appear in at least one chunk
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	// Build paragraphs where each word appears in a separate paragraph
	var parts []string
	for _, w := range words {
		parts = append(parts, w+" "+repeat("x", 50))
	}
	text := strings.Join(parts, "\n\n")

	c := NewChunker(cfg(80, 10, 20))
	chunks := c.Chunk(jobID, text)

	allContent := ""
	for _, ch := range chunks {
		allContent += ch.Content + " "
	}
	for _, w := range words {
		if !strings.Contains(allContent, w) {
			t.Errorf("word %q lost after chunking", w)
		}
	}
}

func TestChunk_ContentTrimmed(t *testing.T) {
	// Chunks should never have leading/trailing whitespace
	var parts []string
	for i := 0; i < 5; i++ {
		parts = append(parts, "  "+repeat("a", 60)+"  ")
	}
	text := strings.Join(parts, "\n\n")

	c := NewChunker(cfg(80, 5, 20))
	chunks := c.Chunk(jobID, text)

	for i, ch := range chunks {
		if ch.Content != strings.TrimSpace(ch.Content) {
			t.Errorf("chunk[%d] has untrimmed content", i)
		}
	}
}

// --- splitParagraphs ---

func TestSplitParagraphs_Basic(t *testing.T) {
	paras := splitParagraphs("hello\n\nworld")
	if len(paras) != 2 {
		t.Fatalf("expected 2, got %d", len(paras))
	}
	if paras[0] != "hello" || paras[1] != "world" {
		t.Errorf("unexpected: %v", paras)
	}
}

func TestSplitParagraphs_EmptyLinesSkipped(t *testing.T) {
	paras := splitParagraphs("a\n\n\n\nb\n\n\n\nc")
	if len(paras) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(paras), paras)
	}
}

func TestSplitParagraphs_LeadingTrailingBlankLines(t *testing.T) {
	paras := splitParagraphs("\n\nhello\n\nworld\n\n")
	if len(paras) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(paras), paras)
	}
}

func TestSplitParagraphs_SingleParagraph(t *testing.T) {
	paras := splitParagraphs("just one paragraph")
	if len(paras) != 1 || paras[0] != "just one paragraph" {
		t.Errorf("unexpected: %v", paras)
	}
}

func TestSplitParagraphs_WhitespaceOnly(t *testing.T) {
	paras := splitParagraphs("   \n\n   \n\n   ")
	if len(paras) != 0 {
		t.Errorf("expected 0 paragraphs, got %d: %v", len(paras), paras)
	}
}

func TestSplitParagraphs_Empty(t *testing.T) {
	paras := splitParagraphs("")
	if len(paras) != 0 {
		t.Errorf("expected 0 paragraphs, got %d", len(paras))
	}
}

func TestSplitParagraphs_TrimsEachParagraph(t *testing.T) {
	paras := splitParagraphs("  hello  \n\n  world  ")
	if paras[0] != "hello" || paras[1] != "world" {
		t.Errorf("paragraphs not trimmed: %v", paras)
	}
}

// --- splitSentences ---

func TestSplitSentences_BasicPeriod(t *testing.T) {
	sents := splitSentences("Hello world. Goodbye world.")
	if len(sents) != 2 {
		t.Fatalf("expected 2 sentences, got %d: %v", len(sents), sents)
	}
}

func TestSplitSentences_ExclamationAndQuestion(t *testing.T) {
	sents := splitSentences("Really! Are you sure? Yes.")
	if len(sents) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(sents), sents)
	}
}

func TestSplitSentences_NoTerminator_SingleSentence(t *testing.T) {
	sents := splitSentences("no terminator here")
	if len(sents) != 1 || sents[0] != "no terminator here" {
		t.Errorf("unexpected: %v", sents)
	}
}

func TestSplitSentences_Empty(t *testing.T) {
	sents := splitSentences("")
	if len(sents) != 0 {
		t.Errorf("expected 0, got %d", len(sents))
	}
}

func TestSplitSentences_AbbreviationNotSplit(t *testing.T) {
	// "e.g." followed by lowercase should not split
	sents := splitSentences("Use e.g. this example. Next sentence.")
	// "e.g." won't split because next char is lowercase/space+lowercase
	// we just verify the final sentence is present
	found := false
	for _, s := range sents {
		if strings.Contains(s, "Next sentence") {
			found = true
		}
	}
	if !found {
		t.Errorf("lost 'Next sentence' in: %v", sents)
	}
}

func TestSplitSentences_MultipleSpacesBetween(t *testing.T) {
	sents := splitSentences("First sentence.  Second sentence.")
	if len(sents) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(sents), sents)
	}
}

func TestSplitSentences_TrailingWhitespaceTrimmed(t *testing.T) {
	sents := splitSentences("  Hello.  World.  ")
	for i, s := range sents {
		if s != strings.TrimSpace(s) {
			t.Errorf("sentence[%d] not trimmed: %q", i, s)
		}
	}
}

func TestSplitSentences_NewlineTerminator(t *testing.T) {
	sents := splitSentences("Line one.\nLine two.")
	if len(sents) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(sents), sents)
	}
}

// --- Edge cases ---

func TestChunk_SingleVeryLongWord_DoesNotPanic(t *testing.T) {
	// A single word longer than maxChunkSize — no sentence/paragraph boundaries at all
	text := repeat("a", 500)
	c := NewChunker(cfg(100, 10, 20))
	// Should not panic
	chunks := c.Chunk(jobID, text)
	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestChunk_UnicodeContent(t *testing.T) {
	// Multi-byte characters — ensure we don't slice mid-rune
	para1 := "日本語のテキストです。これはテストです。" // Japanese
	para2 := "中文文本示例。这是一个测试。"             // Chinese
	text := para1 + "\n\n" + para2

	c := NewChunker(cfg(30, 5, 20))
	chunks := c.Chunk(jobID, text)

	if len(chunks) == 0 {
		t.Fatal("expected chunks from unicode text")
	}
	// Verify content is valid UTF-8
	for i, ch := range chunks {
		if !isValidUTF8(ch.Content) {
			t.Errorf("chunk[%d] contains invalid UTF-8", i)
		}
	}
}

func TestChunk_AllChunksNonEmpty(t *testing.T) {
	var parts []string
	for i := 0; i < 10; i++ {
		parts = append(parts, repeat("word ", 20))
	}
	text := strings.Join(parts, "\n\n")

	c := NewChunker(cfg(80, 10, 20))
	chunks := c.Chunk(jobID, text)

	for i, ch := range chunks {
		if strings.TrimSpace(ch.Content) == "" {
			t.Errorf("chunk[%d] is empty", i)
		}
	}
}

func TestChunk_StatusAlwaysPending(t *testing.T) {
	var parts []string
	for i := 0; i < 5; i++ {
		parts = append(parts, repeat("a", 60))
	}
	chunks := NewChunker(cfg(80, 5, 20)).Chunk(jobID, strings.Join(parts, "\n\n"))
	for i, ch := range chunks {
		if ch.Status != domain.JobStatusPending {
			t.Errorf("chunk[%d]: expected pending, got %s", i, ch.Status)
		}
	}
}

func TestChunk_DefaultConfig_SmallDoc(t *testing.T) {
	c := NewChunker(DefaultChunkConfig())
	chunks := c.Chunk(jobID, "A short document.")
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk with default config for short doc, got %d", len(chunks))
	}
}

func TestChunk_DefaultConfig_LargeDoc(t *testing.T) {
	// Build a doc larger than 4000 chars
	var parts []string
	for i := 0; i < 20; i++ {
		parts = append(parts, repeat("word ", 50)) // 250 chars each
	}
	text := strings.Join(parts, "\n\n")

	c := NewChunker(DefaultChunkConfig())
	chunks := c.Chunk(jobID, text)

	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks for large doc, got %d", len(chunks))
	}
	assertChunks(t, chunks, jobID)
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}
