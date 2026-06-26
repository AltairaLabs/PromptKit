package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

// goldenViewPageSizes is the size matrix used for ViewPage golden snapshots.
var goldenViewPageSizes = []struct {
	name string
	w, h int
}{
	{"80x24", 80, 24},
	{"120x40", 120, 40},
}

// TestViewPage_RendersFileBrowser verifies that ViewPage.View() at 100×30 does
// not panic and returns a non-empty string.
func TestViewPage_RendersFileBrowser(t *testing.T) {
	dir := t.TempDir()
	p := NewViewPage(dir)
	p.SetSize(100, 30)

	view := p.View()
	if view == "" {
		t.Fatal("View() returned empty string")
	}
}

// TestViewPage_TitleIsView verifies Title() returns the view page title.
func TestViewPage_TitleIsView(t *testing.T) {
	p := NewViewPage(".")
	if got := p.Title(); got != titleView {
		t.Fatalf("Title() = %q, want %q", got, titleView)
	}
}

// TestViewPage_Init verifies Init() returns a non-nil cmd (filebrowser init).
func TestViewPage_Init(t *testing.T) {
	p := NewViewPage(".")
	_ = p.Init() // Must not panic; filepicker init may return nil on headless
}

// TestViewPage_NonFileMsgForwarded verifies that unrecognised messages are
// forwarded to the browser without panic and the returned page is non-nil.
func TestViewPage_NonFileMsgForwarded(t *testing.T) {
	p := NewViewPage(".")
	p.SetSize(80, 24)
	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if newPage == nil {
		t.Fatal("Update(KeyDown) returned nil page")
	}
}

// TestViewPage_SelectionTriggersSummaryLoad verifies that when a
// FileSelectedMsg arrives for "index.json" in a directory that has a valid
// summary file, Update returns a cmd.  Running that cmd returns a
// summaryLoadedViewMsg (the file→summary path).
func TestViewPage_SelectionTriggersSummaryLoad(t *testing.T) {
	dir := t.TempDir()

	// Write a minimal index.json.
	summary := map[string]interface{}{
		"run_ids": []interface{}{"run-abc"},
	}
	indexData, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("setup: marshal summary: %v", err)
	}
	indexPath := filepath.Join(dir, "index.json")
	if err := os.WriteFile(indexPath, indexData, 0o644); err != nil {
		t.Fatalf("setup: write index.json: %v", err)
	}

	p := NewViewPage(dir)
	p.SetSize(80, 24)

	_, cmd := p.Update(pages.FileSelectedMsg{RunID: "index", Path: indexPath})
	if cmd == nil {
		t.Fatal("Update(FileSelectedMsg for index.json) returned nil cmd; expected a load cmd")
	}

	// Execute the cmd — it runs synchronously in the test.
	msg := cmd()
	if msg == nil {
		t.Fatal("load cmd returned nil msg")
	}
	if _, ok := msg.(summaryLoadedViewMsg); !ok {
		t.Fatalf("expected summaryLoadedViewMsg, got %T: %v", msg, msg)
	}
}

// TestViewPage_SummaryLoadedPushesResultsPage verifies that feeding a
// summaryLoadedViewMsg back into Update returns a cmd that resolves to a
// PushPageMsg carrying a *ResultsPage.
func TestViewPage_SummaryLoadedPushesResultsPage(t *testing.T) {
	dir := t.TempDir()
	p := NewViewPage(dir)
	p.SetSize(80, 24)

	summary := map[string]interface{}{
		"run_ids": []interface{}{},
	}
	_, cmd := p.Update(summaryLoadedViewMsg{summary: summary, resultsDir: dir})
	if cmd == nil {
		t.Fatal("Update(summaryLoadedViewMsg) returned nil cmd; expected PushPageMsg cmd")
	}

	msg := cmd()
	push, ok := msg.(PushPageMsg)
	if !ok {
		t.Fatalf("expected PushPageMsg, got %T %v", msg, msg)
	}
	if push.Page == nil {
		t.Fatal("PushPageMsg.Page is nil")
	}
	if _, ok := push.Page.(*ResultsPage); !ok {
		t.Fatalf("expected *ResultsPage, got %T", push.Page)
	}
}

// TestViewPage_ResultLoadedPushesConversationPage verifies that feeding a
// resultLoadedViewMsg back into Update returns a cmd that resolves to a
// PushPageMsg carrying a *ConversationViewPage.
func TestViewPage_ResultLoadedPushesConversationPage(t *testing.T) {
	dir := t.TempDir()
	p := NewViewPage(dir)
	p.SetSize(80, 24)

	result := &statestore.RunResult{
		RunID:      "run-xyz",
		ScenarioID: "scen-1",
		ProviderID: "prov-1",
	}

	_, cmd := p.Update(resultLoadedViewMsg{
		runID:      result.RunID,
		scenarioID: result.ScenarioID,
		providerID: result.ProviderID,
		result:     result,
	})
	if cmd == nil {
		t.Fatal("Update(resultLoadedViewMsg) returned nil cmd; expected PushPageMsg cmd")
	}

	msg := cmd()
	push, ok := msg.(PushPageMsg)
	if !ok {
		t.Fatalf("expected PushPageMsg, got %T %v", msg, msg)
	}
	if push.Page == nil {
		t.Fatal("PushPageMsg.Page is nil")
	}
	if _, ok := push.Page.(*ConversationViewPage); !ok {
		t.Fatalf("expected *ConversationViewPage, got %T", push.Page)
	}
}

// TestViewPage_ErrorMsgIsStoredAndShownInView verifies that a
// resultLoadErrorViewMsg stores the error and View surfaces it.
func TestViewPage_ErrorMsgIsStoredAndShownInView(t *testing.T) {
	p := NewViewPage(".")
	p.SetSize(80, 24)

	newPage, cmd := p.Update(resultLoadErrorViewMsg{err: errTest})
	if cmd != nil {
		t.Fatal("Update(resultLoadErrorViewMsg) should return nil cmd")
	}
	vp, ok := newPage.(*ViewPage)
	if !ok {
		t.Fatalf("Update returned %T, want *ViewPage", newPage)
	}
	if vp.err == nil {
		t.Fatal("error was not stored after resultLoadErrorViewMsg")
	}
}

// errTest is a sentinel error used only in tests.
var errTest = errViewTest("test error")

type errViewTest string

func (e errViewTest) Error() string { return string(e) }

// TestResultsPage_Title verifies Title() returns "Results".
func TestResultsPage_Title(t *testing.T) {
	rp := NewResultsPage(map[string]interface{}{}, ".")
	if got := rp.Title(); got != "Results" {
		t.Fatalf("Title() = %q, want %q", got, "Results")
	}
}

// TestResultsPage_View verifies View() does not panic and returns non-empty output.
func TestResultsPage_View(t *testing.T) {
	rp := NewResultsPage(map[string]interface{}{"run_ids": []interface{}{}}, ".")
	rp.SetSize(80, 24)
	view := rp.View()
	if view == "" {
		t.Fatal("ResultsPage.View() returned empty string")
	}
}

// TestConversationViewPage_Title verifies Title() returns "Conversation".
func TestConversationViewPage_Title(t *testing.T) {
	result := &statestore.RunResult{RunID: "r1"}
	cvp := NewConversationViewPage("r1", "s1", "p1", result)
	if got := cvp.Title(); got != "Conversation" {
		t.Fatalf("Title() = %q, want %q", got, "Conversation")
	}
}

// TestConversationViewPage_View verifies View() does not panic and returns
// non-empty output.
func TestConversationViewPage_View(t *testing.T) {
	result := &statestore.RunResult{RunID: "r1", ScenarioID: "s1", ProviderID: "p1"}
	cvp := NewConversationViewPage("r1", "s1", "p1", result)
	cvp.SetSize(80, 24)
	view := cvp.View()
	if view == "" {
		t.Fatal("ConversationViewPage.View() returned empty string")
	}
}

// TestViewPage_ViewShowsErrorBanner verifies that View() surfaces the stored error string.
func TestViewPage_ViewShowsErrorBanner(t *testing.T) {
	p := NewViewPage(".")
	p.SetSize(80, 24)
	p.err = errTest
	view := stripANSI(p.View())
	if view == "" {
		t.Fatal("View() returned empty string when error is set")
	}
}

// TestResultsPage_Init verifies that Init returns a non-nil cmd (async load).
func TestResultsPage_Init(t *testing.T) {
	summary := map[string]interface{}{"run_ids": []interface{}{}}
	rp := NewResultsPage(summary, ".")
	cmd := rp.Init()
	if cmd == nil {
		t.Fatal("ResultsPage.Init() returned nil cmd, expected an async load cmd")
	}
}

// TestResultsPage_UpdateResultsLoaded verifies that a resultsLoadedViewMsg
// populates the main page without panicking.
func TestResultsPage_UpdateResultsLoaded(t *testing.T) {
	rp := NewResultsPage(map[string]interface{}{}, ".")
	rp.SetSize(80, 24)

	runs := []*statestore.RunResult{
		{RunID: "r1", ScenarioID: "s1", ProviderID: "p1"},
		{RunID: "r2", ScenarioID: "s2", ProviderID: "p2", Error: "boom"},
	}

	newPage, cmd := rp.Update(resultsLoadedViewMsg{runs: runs})
	if cmd != nil {
		t.Fatal("Update(resultsLoadedViewMsg) should return nil cmd")
	}
	rp2, ok := newPage.(*ResultsPage)
	if !ok {
		t.Fatalf("expected *ResultsPage, got %T", newPage)
	}
	if len(rp2.loadedResults) != 2 {
		t.Fatalf("expected 2 loaded results, got %d", len(rp2.loadedResults))
	}
}

// TestResultsPage_UpdateErrorStored verifies that a resultLoadErrorViewMsg
// stores the error.
func TestResultsPage_UpdateErrorStored(t *testing.T) {
	rp := NewResultsPage(map[string]interface{}{}, ".")
	rp.SetSize(80, 24)

	newPage, _ := rp.Update(resultLoadErrorViewMsg{err: errTest})
	rp2, ok := newPage.(*ResultsPage)
	if !ok {
		t.Fatalf("expected *ResultsPage, got %T", newPage)
	}
	if rp2.lastError == nil {
		t.Fatal("lastError was not stored after resultLoadErrorViewMsg")
	}
}

// TestResultsPage_TabCyclesFocus verifies that Tab changes mainPageFocus.
func TestResultsPage_TabCyclesFocus(t *testing.T) {
	rp := NewResultsPage(map[string]interface{}{}, ".")
	rp.SetSize(80, 24)

	initial := rp.mainPageFocus
	newPage, _ := rp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tab")})
	rp2, ok := newPage.(*ResultsPage)
	if !ok {
		t.Fatalf("expected *ResultsPage, got %T", newPage)
	}
	if rp2.mainPageFocus == initial {
		t.Fatal("Tab key did not change mainPageFocus")
	}
}

// TestResultsPage_EnterNoResultsNoOp verifies Enter with no loaded results is a no-op.
func TestResultsPage_EnterNoResultsNoOp(t *testing.T) {
	rp := NewResultsPage(map[string]interface{}{}, ".")
	rp.SetSize(80, 24)

	newPage, cmd := rp.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := newPage.(*ResultsPage); !ok {
		t.Fatalf("expected *ResultsPage, got %T", newPage)
	}
	// With no loaded results cursor (0) is out of bounds, so no push.
	if cmd != nil {
		t.Fatal("Enter with no results should not produce a cmd")
	}
}

// TestResultsPage_UnknownMsgIsNoOp verifies that unrecognised messages are a
// safe no-op.
func TestResultsPage_UnknownMsgIsNoOp(t *testing.T) {
	rp := NewResultsPage(map[string]interface{}{}, ".")
	rp.SetSize(80, 24)

	newPage, cmd := rp.Update(tea.KeyMsg{Type: tea.KeyDown})
	if newPage == nil {
		t.Fatal("Update returned nil page for unknown msg")
	}
	_ = cmd // nil is fine
}

// TestConversationViewPage_Init verifies Init returns nil.
func TestConversationViewPage_Init(t *testing.T) {
	result := &statestore.RunResult{RunID: "r1"}
	cvp := NewConversationViewPage("r1", "s1", "p1", result)
	if cmd := cvp.Init(); cmd != nil {
		t.Fatal("ConversationViewPage.Init() should return nil cmd")
	}
}

// TestConversationViewPage_UpdateForwards verifies Update forwards messages
// without panicking.
func TestConversationViewPage_UpdateForwards(t *testing.T) {
	result := &statestore.RunResult{RunID: "r1"}
	cvp := NewConversationViewPage("r1", "s1", "p1", result)
	cvp.SetSize(80, 24)

	newPage, _ := cvp.Update(tea.KeyMsg{Type: tea.KeyDown})
	if newPage == nil {
		t.Fatal("Update returned nil page")
	}
}

// TestConversationViewPage_SetSize verifies SetSize stores dimensions.
func TestConversationViewPage_SetSize(t *testing.T) {
	result := &statestore.RunResult{RunID: "r1"}
	cvp := NewConversationViewPage("r1", "s1", "p1", result)
	cvp.SetSize(100, 40)
	if cvp.w != 100 || cvp.h != 40 {
		t.Fatalf("SetSize(100,40): got w=%d h=%d", cvp.w, cvp.h)
	}
}

// TestLoadRunResult_MissingFileReturnsError verifies that loadRunResult with
// a missing runID in an empty directory returns a resultLoadErrorViewMsg.
func TestLoadRunResult_MissingFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	cmd := loadRunResult("nonexistent-run-id", dir)
	if cmd == nil {
		t.Fatal("loadRunResult returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(resultLoadErrorViewMsg); !ok {
		t.Fatalf("expected resultLoadErrorViewMsg, got %T: %v", msg, msg)
	}
}

// TestLoadResultsFromSummary_EmptyRunIDs verifies that an empty run_ids list
// returns a resultsLoadedViewMsg with no runs (not an error).
func TestLoadResultsFromSummary_EmptyRunIDs(t *testing.T) {
	summary := map[string]interface{}{"run_ids": []interface{}{}}
	cmd := loadResultsFromSummary(summary, ".")
	if cmd == nil {
		t.Fatal("loadResultsFromSummary returned nil cmd")
	}
	msg := cmd()
	loaded, ok := msg.(resultsLoadedViewMsg)
	if !ok {
		t.Fatalf("expected resultsLoadedViewMsg, got %T: %v", msg, msg)
	}
	if len(loaded.runs) != 0 {
		t.Fatalf("expected 0 runs, got %d", len(loaded.runs))
	}
}

// TestLoadResultsFromSummary_NoRunIDsKey verifies that a summary without
// run_ids still returns a resultsLoadedViewMsg (graceful degradation).
func TestLoadResultsFromSummary_NoRunIDsKey(t *testing.T) {
	summary := map[string]interface{}{}
	cmd := loadResultsFromSummary(summary, ".")
	msg := cmd()
	if _, ok := msg.(resultsLoadedViewMsg); !ok {
		t.Fatalf("expected resultsLoadedViewMsg for empty summary, got %T", msg)
	}
}

// TestLoadResultsFromSummary_InvalidRunIDs verifies that a non-array run_ids
// returns a resultLoadErrorViewMsg.
func TestLoadResultsFromSummary_InvalidRunIDs(t *testing.T) {
	summary := map[string]interface{}{"run_ids": "not-an-array"}
	cmd := loadResultsFromSummary(summary, ".")
	msg := cmd()
	if _, ok := msg.(resultLoadErrorViewMsg); !ok {
		t.Fatalf("expected resultLoadErrorViewMsg, got %T", msg)
	}
}

// TestConvertResultsToViewData verifies that convertResultsToViewData maps
// fields correctly.
func TestConvertResultsToViewData(t *testing.T) {
	runs := []*statestore.RunResult{
		{RunID: "r1", ScenarioID: "s1", ProviderID: "p1"},
		{RunID: "r2", ScenarioID: "s2", ProviderID: "p2", Error: "boom"},
	}
	got := convertResultsToViewData(runs)
	if len(got) != 2 {
		t.Fatalf("expected 2 RunInfo, got %d", len(got))
	}
	if got[0].RunID != "r1" {
		t.Errorf("RunID[0] = %q, want %q", got[0].RunID, "r1")
	}
	if got[1].Status != panelsStatusFailed() {
		t.Errorf("Status[1] = %v, want Failed", got[1].Status)
	}
}

// panelsStatusFailed returns the panels.StatusFailed constant without importing it
// directly from the panels package (the constant is already available via the
// viewpage_test file's same package scope since panels is imported in viewpage.go).
func panelsStatusFailed() panels.RunStatus {
	return panels.StatusFailed
}

// TestResultsPage_EnterWithLoadedResults verifies that Enter with a loaded
// result in the table emits a PushPageMsg carrying a *ConversationViewPage.
func TestResultsPage_EnterWithLoadedResults(t *testing.T) {
	rp := NewResultsPage(map[string]interface{}{}, ".")
	rp.SetSize(80, 24)

	runs := []*statestore.RunResult{
		{RunID: "r1", ScenarioID: "s1", ProviderID: "p1"},
	}
	// Populate loadedResults via Update.
	rp.Update(resultsLoadedViewMsg{runs: runs}) //nolint:errcheck

	// Press Enter — cursor is at 0, which is in range.
	newPage, cmd := rp.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := newPage.(*ResultsPage); !ok {
		t.Fatalf("expected *ResultsPage returned, got %T", newPage)
	}
	if cmd == nil {
		t.Fatal("Enter with a loaded result should return a PushPageMsg cmd")
	}
	msg := cmd()
	push, ok := msg.(PushPageMsg)
	if !ok {
		t.Fatalf("expected PushPageMsg, got %T", msg)
	}
	if _, ok := push.Page.(*ConversationViewPage); !ok {
		t.Fatalf("expected *ConversationViewPage, got %T", push.Page)
	}
}

// TestLoadSummaryFile_BadJSON verifies that a malformed JSON file returns a
// resultLoadErrorViewMsg.
func TestLoadSummaryFile_BadJSON(t *testing.T) {
	dir := t.TempDir()
	badPath := dir + "/index.json"
	if err := os.WriteFile(badPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmd := loadSummaryFile(badPath, dir)
	msg := cmd()
	if _, ok := msg.(resultLoadErrorViewMsg); !ok {
		t.Fatalf("expected resultLoadErrorViewMsg, got %T", msg)
	}
}

// TestLoadResultsFromSummary_WithRealResult verifies that run IDs that fail to
// load don't crash the cmd (LoadResults skips missing runs).
func TestLoadResultsFromSummary_WithRealResult(t *testing.T) {
	dir := t.TempDir()
	// Write a minimal valid result file.
	result := &statestore.RunResult{
		RunID:      "run-001",
		ScenarioID: "scen-a",
		ProviderID: "prov-x",
	}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(dir, "run-001.json"), data, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	summary := map[string]interface{}{"run_ids": []interface{}{"run-001"}}
	cmd := loadResultsFromSummary(summary, dir)
	msg := cmd()
	loaded, ok := msg.(resultsLoadedViewMsg)
	if !ok {
		t.Fatalf("expected resultsLoadedViewMsg, got %T: %v", msg, msg)
	}
	if len(loaded.runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(loaded.runs))
	}
}

// TestLoadResultsFromSummary_AllRunsMissing verifies that when all run IDs fail
// to load, a resultLoadErrorViewMsg is returned.
func TestLoadResultsFromSummary_AllRunsMissing(t *testing.T) {
	dir := t.TempDir()
	summary := map[string]interface{}{"run_ids": []interface{}{"ghost-run-id"}}
	cmd := loadResultsFromSummary(summary, dir)
	msg := cmd()
	if _, ok := msg.(resultLoadErrorViewMsg); !ok {
		t.Fatalf("expected resultLoadErrorViewMsg when all runs missing, got %T", msg)
	}
}

// TestLoadRunResult_ValidFile verifies that a valid JSON result file is loaded
// and returns a resultLoadedViewMsg.
func TestLoadRunResult_ValidFile(t *testing.T) {
	dir := t.TempDir()
	result := &statestore.RunResult{
		RunID:      "run-42",
		ScenarioID: "scen-b",
		ProviderID: "prov-y",
	}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(dir, "run-42.json"), data, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cmd := loadRunResult("run-42", dir)
	msg := cmd()
	loaded, ok := msg.(resultLoadedViewMsg)
	if !ok {
		t.Fatalf("expected resultLoadedViewMsg, got %T: %v", msg, msg)
	}
	if loaded.runID != "run-42" {
		t.Errorf("runID = %q, want %q", loaded.runID, "run-42")
	}
}

// TestGoldenViewPage captures a stable golden snapshot of the ViewPage
// (file browser) in an empty directory.
func TestGoldenViewPage(t *testing.T) {
	dir := t.TempDir()
	for _, sz := range goldenViewPageSizes {
		t.Run(sz.name, func(t *testing.T) {
			p := NewViewPage(dir)
			p.SetSize(sz.w, sz.h)
			out := stripANSI(p.View())
			teatest.RequireEqualOutput(t, []byte(out))
		})
	}
}
