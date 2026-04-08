package dashboard

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Chemaclass/seed-hunter/internal/pipeline"
)

func baseMeta() Meta {
	return Meta{
		TemplateHash:  "9f2eab10cafebabedeadbeef",
		Position:      3,
		API:           "mempool",
		ScriptType:    "segwit",
		DeriveWorkers: 8,
		APIWorkers:    2,
		RateLimit:     2.0,
		NAddresses:    3,
	}
}

func TestRenderShowsProcessedAndPercentage(t *testing.T) {
	f := Frame{
		Meta:      baseMeta(),
		Resumed:   false,
		ResumedAt: -1,
		Processed: 512,
		Elapsed:   60 * time.Second,
	}
	out := Render(f)
	if !strings.Contains(out, "512 / 2048") {
		t.Errorf("expected output to contain %q, got:\n%s", "512 / 2048", out)
	}
	if !strings.Contains(out, "25.00%") {
		t.Errorf("expected output to contain %q, got:\n%s", "25.00%", out)
	}
}

func TestRenderShowsResumeOffsetWhenResumed(t *testing.T) {
	// totalProcessed = ResumedAt + 1 + Processed = 311 + 1 + 100 = 412
	f := Frame{
		Meta:      baseMeta(),
		Resumed:   true,
		ResumedAt: 311,
		Processed: 100,
		Elapsed:   60 * time.Second,
	}
	out := Render(f)
	if !strings.Contains(out, "412 / 2048") {
		t.Errorf("expected output to contain %q, got:\n%s", "412 / 2048", out)
	}
	if !strings.Contains(out, "[resumed at 311]") {
		t.Errorf("expected output to contain %q, got:\n%s", "[resumed at 311]", out)
	}
	if !strings.Contains(strings.ToLower(out), "resumed") {
		t.Errorf("expected output to mention 'resumed', got:\n%s", out)
	}
}

func TestRenderRateAndETAFromElapsed(t *testing.T) {
	f := Frame{
		Meta:      baseMeta(),
		Resumed:   false,
		ResumedAt: -1,
		Processed: 200,
		Elapsed:   100 * time.Second,
	}
	out := Render(f)
	if !strings.Contains(out, "2.00 attempts/s") {
		t.Errorf("expected rate %q in output, got:\n%s", "2.00 attempts/s", out)
	}
	if !strings.Contains(out, "ETA position") {
		t.Errorf("expected 'ETA position' line in output, got:\n%s", out)
	}
}

func TestRenderHandlesZeroRateGracefully(t *testing.T) {
	f := Frame{
		Meta:      baseMeta(),
		Resumed:   false,
		ResumedAt: -1,
		Processed: 0,
		Elapsed:   0,
	}
	out := Render(f)
	for _, bad := range []string{"+Inf", "NaN", "Inf"} {
		if strings.Contains(out, bad) {
			t.Errorf("output must not contain %q, got:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "—") && !strings.Contains(out, "-") {
		t.Errorf("expected a placeholder for unknown values, got:\n%s", out)
	}
}

func TestRenderShowsFullKeyspaceETAYears(t *testing.T) {
	f := Frame{
		Meta:      baseMeta(),
		Resumed:   false,
		ResumedAt: -1,
		Processed: 200,
		Elapsed:   100 * time.Second,
	}
	out := Render(f)
	// Extract the ETA full key line to avoid false matches from elsewhere.
	var etaLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "ETA full key") {
			etaLine = line
			break
		}
	}
	if etaLine == "" {
		t.Fatalf("expected an 'ETA full key' line in output, got:\n%s", out)
	}
	if !strings.Contains(etaLine, "e+") {
		t.Errorf("expected scientific notation ('e+') in ETA full key, got: %q", etaLine)
	}
	if !strings.Contains(etaLine, "years") {
		t.Errorf("expected 'years' unit in ETA full key, got: %q", etaLine)
	}
}

func TestRenderOmitsResumeBadgeWhenFresh(t *testing.T) {
	f := Frame{
		Meta:      baseMeta(),
		Resumed:   false,
		ResumedAt: -1,
		Processed: 50,
		Elapsed:   10 * time.Second,
	}
	out := Render(f)
	if strings.Contains(strings.ToLower(out), "resumed") {
		t.Errorf("fresh run must not contain 'resumed', got:\n%s", out)
	}
}

func TestRenderShowsTruncatedTemplateHash(t *testing.T) {
	meta := baseMeta()
	meta.TemplateHash = "9f2eab10cafebabedeadbeef0123456789"
	f := Frame{
		Meta:      meta,
		Resumed:   false,
		ResumedAt: -1,
		Processed: 10,
		Elapsed:   1 * time.Second,
	}
	out := Render(f)
	if !strings.Contains(out, "9f2eab10") {
		t.Errorf("expected truncated hash %q in output, got:\n%s", "9f2eab10", out)
	}
	if strings.Contains(out, "9f2eab10cafebabedeadbeef0123456789") {
		t.Errorf("output must not contain full template hash, got:\n%s", out)
	}
}

func TestRunRepaintLoopWritesAtLeastOneFrameThenStops(t *testing.T) {
	stats := pipeline.NewStats()
	stats.SessionID.Store(7)
	stats.Processed.Store(42)
	stats.ValidMnemonics.Store(40)
	stats.Hits.Store(0)
	stats.Errors.Store(1)

	meta := baseMeta()

	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, &buf, meta, stats, 10*time.Millisecond)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}

	if buf.Len() == 0 {
		t.Fatal("expected Run to write at least one frame, got empty buffer")
	}
	if !strings.Contains(buf.String(), "attempts") {
		t.Errorf("expected rendered frame to contain 'attempts', got:\n%s", buf.String())
	}
}
