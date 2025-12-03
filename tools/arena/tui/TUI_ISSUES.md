# TUI Code Quality Issues

## Summary
This document catalogs dead code, race conditions, and design issues found in the TUI package.

---

## ✅ FIXED CRITICAL ISSUES

### 1. Race Condition in ConversationPane - ✅ FIXED
**Location**: `conversation_pane.go:44-57`  
**Severity**: HIGH  
**Status**: **FIXED**

**Problem**: `ConversationPane` struct had no mutex protection but was accessed from multiple contexts.

**Solution Implemented**: Removed mutex from ConversationPane entirely. All methods now document that the caller must hold Model.mu. Since ConversationPane is only accessed from Model methods that already hold the lock, this eliminates the deadlock while maintaining thread safety.

```go
// ConversationPane encapsulates the conversation view state (table + detail).
// All methods assume the caller holds Model.mu for thread safety.
type ConversationPane struct {
    // No mutex needed - protected by Model.mu
    focus       conversationFocus
    // ... rest
}
```

---

## ✅ FIXED HIGH PRIORITY

### 2. Dead Code: renderMetrics() Function - ✅ FIXED
**Location**: `metrics_panel.go:16`  
**Status**: **FIXED** - Function removed, tests removed

**Problem**: `renderMetrics()` was never called in production code, only in tests.

**Solution**: Removed `renderMetrics()` function entirely. Removed tests `TestRenderMetrics_Content` and `TestModel_renderMetrics`. Metrics are now displayed only via the summary pane.

---

## ✅ FIXED MEDIUM PRIORITY

### 3. Overly Broad Panic Recovery - ✅ FIXED
**Location**: `log_interceptor.go:92-99`  
**Status**: **FIXED**

**Problem**: Used `recover()` to silently ignore ALL panics from `program.Send()`:
```go
func() {
    defer func() {
        _ = recover() // Swallows ALL panics!
    }()
    l.program.Send(LogMsg{...})
}()
```

**Issue**: This will hide bugs and unexpected errors, not just bubbletea-specific ones.

**Solution Implemented**: Now logs panics to stderr instead of silently ignoring:
```go
defer func() {
    if r := recover(); r != nil {
        // Log panic to stderr instead of silently ignoring
        fmt.Fprintf(os.Stderr, "panic sending log to TUI: %v\n", r)
    }
}()
```

---

### 4. Context.Background() in Render Functions - ✅ FIXED
**Locations**: All render functions  
**Status**: **FIXED**

**Problem**: Used `context.Background()` instead of propagating proper context.

**Solution Implemented**: Added `ctx` field to Model struct, initialized in NewModel(). All render functions now use `m.ctx` (with fallback to Background() if nil):
```go
ctx := m.ctx
if ctx == nil {
    ctx = context.Background()
}
res, err := m.stateStore.GetResult(ctx, selected.RunID)
```

---

### 5. Unused Field: Model.totalTokens - ✅ FIXED
**Location**: `tui.go`  
**Status**: **FIXED** - Field removed

**Problem**: `totalTokens` field was never populated in production code.

**Solution**: Removed field from Model struct. BuildSummary now returns 0 for tokens when not using statestore (tokens are only available via buildSummaryFromStateStore which computes them from results).

---

## 🟢 LOW PRIORITY

### 6. Empty Struct Pattern: MainPage
**Location**: `main_page.go:8`

**Code**:
```go
type MainPage struct{}

func (MainPage) Render(m *Model) string {
    // ...
}
```

**Issue**: Empty struct with single method - could just be a function.

**Why it exists**: Possibly for interface satisfaction or future extensibility.

**Recommendation**: Either:
1. Convert to regular function if no interface requirement
2. Add comment explaining why it's a type

---

### 7. Int-based page/pane types lack String() methods
**Location**: `tui.go:59-69`

**Code**:
```go
type pane int
const (
    paneRuns pane = iota
    paneLogs
)

type page int
const (
    pageMain page = iota
    pageConversation
)
```

**Issue**: Hard to debug without string representation.

**Fix**: Add String() methods:
```go
func (p pane) String() string {
    switch p {
    case paneRuns: return "runs"
    case paneLogs: return "logs"
    default: return fmt.Sprintf("unknown(%d)", p)
    }
}
```

---

### 8. Thin Wrapper Functions
**Functions**: `renderResultPane()`, `renderSummaryPane()`

**Locations**: Used only in `main_page.go`

**Issue**: Very thin wrappers that just call other methods.

**Options**:
1. Inline into `MainPage.Render()` for clarity
2. Keep for abstraction/testing

**Not critical** - borderline design choice.

---

## ✅ COMPLETED FIXES

### ✅ 1. Dead ShowSummaryMsg Code - REMOVED
**Status**: **FIXED**

**Removed**:
- `ShowSummaryMsg` type definition from `messages.go`
- `handleShowSummary()` method from `tui.go`
- Summary display logic from `View()` method
- `summary` and `showSummary` fields from `Model` struct
- Related tests in `summary_test.go`

**Reason**: No production code ever sent this message. Summary functionality exists via `BuildSummary()` but the message-based display was unused.

---

## 📊 OVERALL ASSESSMENT

| Category | Count | Status |
|----------|-------|--------|
| Race Conditions | 1 | 🔴 Needs Fix |
| Dead Code | 1 | ✅ Fixed |
| Error Handling Issues | 1 | 🟡 Should Fix |
| Context Handling | 3 | 🟡 Should Fix |
| Unused Fields | 1 | 🟡 Verify |
| Design Questions | 3 | 🟢 Optional |

---

## ✅ STRENGTHS

The TUI codebase has several positive qualities:
- ✅ Good mutex discipline in main Model type
- ✅ All message types (except ShowSummaryMsg) are actively used
- ✅ No TODO/FIXME comments (clean codebase)
- ✅ Helper functions are all properly utilized
- ✅ Comprehensive test coverage

---

## 🎯 RECOMMENDED ACTION PLAN

1. **Immediate**: Fix ConversationPane race condition (add mutex)
2. **Short term**: Fix panic recovery and context handling
3. **Code review**: Decide on totalTokens field (keep or remove)
4. **Optional**: Clean up thin wrappers and add String() methods
5. **Optional**: Review if renderMetrics() should be integrated or removed

---

## Notes

Analysis performed: December 3, 2025  
Files analyzed: All .go files in `tools/arena/tui/`  
Method: Static analysis + usage pattern review
