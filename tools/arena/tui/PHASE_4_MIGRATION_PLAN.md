# Phase 4: Complete Views Migration

## Problem Identified
After completing Phases 1-3 of the MVVM refactoring, significant duplication and misplaced code remains:

1. **`summary.go`** - Contains `RenderSummary()` that duplicates `viewmodels.SummaryViewModel` logic
2. **`runs_panel.go`** - Contains rendering logic that duplicates `views.RunsTableView`
3. **`result_panel.go`** - Pure rendering component in wrong location
4. **`header_footer.go`** - Pure rendering component in wrong location

## Architecture Violations

### Current Issues:
- **Duplication**: Two implementations of summary rendering (old vs ViewModel)
- **Inconsistency**: Some views use MVVM pattern, others use old direct rendering
- **Location**: Pure rendering functions mixed with stateful components
- **Testing**: Old rendering code lacks test coverage

### Correct Pattern:
```
theme/          → Styling primitives (DONE ✅)
viewmodels/     → Data transformation (DONE ✅)
views/          → Pure rendering (PARTIAL ⚠️)
tui/            → Stateful coordination (NEEDS CLEANUP ⚠️)
```

## Migration Plan

### Phase 4.1: Create Missing Views ⏳

1. **SummaryView** - Migrate `RenderSummary()` to use `SummaryViewModel`
   - File: `tools/arena/tui/views/summary.go`
   - Replace 239 lines of summary.go with clean view
   - Use `viewmodels.SummaryViewModel` for all data
   - Tests: `tools/arena/tui/views/summary_test.go`

2. **ResultView** - Extract from `result_panel.go`
   - File: `tools/arena/tui/views/result.go`
   - Pure rendering function for run results
   - Takes data as parameters, returns styled string
   - Tests: `tools/arena/tui/views/result_test.go`

3. **HeaderFooterView** - Extract from `header_footer.go`
   - File: `tools/arena/tui/views/header_footer.go`
   - Pure rendering for header and footer
   - Takes progress, elapsed time as parameters
   - Tests: `tools/arena/tui/views/header_footer_test.go`

### Phase 4.2: Update Main TUI Package ⏳

1. **Update `runs_panel.go`**
   - Remove `renderActiveRuns()` implementation
   - Replace with call to `views.RunsTableView`
   - Use `viewmodels.RunsTableViewModel` for data
   - Keep only coordination logic

2. **Replace `summary.go`**
   - Remove entire file (239 lines)
   - Update `tui.go` to use `views.SummaryView`
   - Update `tui.go` to use `viewmodels.SummaryViewModel`

3. **Update `result_panel.go`**
   - Keep `selectedRun()` helper (coordination)
   - Remove rendering implementation
   - Replace with call to `views.ResultView`

4. **Update `header_footer.go`**
   - Remove rendering implementations
   - Replace with calls to `views.HeaderFooterView`
   - Keep only Model method wrappers if needed

### Phase 4.3: Cleanup & Verification ⏳

1. **Remove Dead Code**
   - Delete old `summary.go` (239 lines)
   - Delete old rendering functions from panels
   - Keep only coordination logic in tui package

2. **Update Tests**
   - Migrate tests from `summary_test.go` to views package
   - Update tui integration tests to use new views
   - Ensure 100% coverage on all new view code

3. **Verify Build & Tests**
   - `make build-arena` - Must succeed
   - `go test ./tools/arena/tui/... -cover` - All pass, >80% coverage
   - `golangci-lint run ./tools/arena/tui/...` - Zero errors
   - Manual TUI test with `./bin/promptarena`

## Expected Benefits

### Code Reduction:
- Remove ~350 lines of duplicate/misplaced code
- Consolidate rendering in `views/` package
- Eliminate inconsistencies between old and new patterns

### Maintainability:
- Single source of truth for each view
- Consistent MVVM pattern throughout
- Clear separation: theme → viewmodels → views → coordination

### Testing:
- 100% coverage on all view rendering
- Isolated view tests (no state dependencies)
- Easier to test edge cases

### Performance:
- No runtime impact (same rendering paths)
- Potential compiler optimizations with cleaner structure

## Files to Create:
- `tools/arena/tui/views/summary.go` (~80 lines)
- `tools/arena/tui/views/summary_test.go` (~120 lines)
- `tools/arena/tui/views/result.go` (~60 lines)
- `tools/arena/tui/views/result_test.go` (~80 lines)
- `tools/arena/tui/views/header_footer.go` (~70 lines)
- `tools/arena/tui/views/header_footer_test.go` (~100 lines)

## Files to Modify:
- `tools/arena/tui/runs_panel.go` - Use RunsTableView
- `tools/arena/tui/tui.go` - Use new views throughout
- `tools/arena/tui/result_panel.go` - Simplify to coordination only

## Files to Delete:
- `tools/arena/tui/summary.go` - Replaced by views/summary.go
- `tools/arena/tui/summary_test.go` - Migrated to views package
- `tools/arena/tui/header_footer.go` - Replaced by views/header_footer.go

## Success Criteria:
- ✅ All new views in `views/` package with 100% test coverage
- ✅ Zero duplication between old and new rendering code
- ✅ All TUI integration tests pass
- ✅ Build successful with zero linting errors
- ✅ Manual TUI test shows correct rendering
- ✅ Overall code coverage >80%

## Risk Assessment:
- **Low Risk**: Pure refactoring, no logic changes
- **Mitigation**: Comprehensive tests at each step
- **Rollback**: Git history preserves all previous states

## Estimated Effort:
- Phase 4.1: 2-3 hours (create 6 new files)
- Phase 4.2: 1-2 hours (update existing files)
- Phase 4.3: 1 hour (cleanup, verification)
- **Total: 4-6 hours**
