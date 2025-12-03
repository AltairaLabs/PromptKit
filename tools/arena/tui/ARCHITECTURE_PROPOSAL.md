# TUI Architecture Refactoring Proposal

## Current Problems

### 1. **Tight Coupling**
- Model contains both state AND rendering logic
- 17 render methods scattered across Model and helper files
- Business logic mixed with presentation (e.g., `BuildSummary` called from View)
- ConversationPane mixes state and rendering

### 2. **Unclear Responsibilities**
- `Model` does everything: state management, event handling, rendering, layout
- Panel files (`runs_panel.go`, `logs_panel.go`) are just render methods on Model
- No clear separation between data transformation and presentation

### 3. **Testing Difficulties**
- Can't test rendering without full Model setup
- Can't test business logic without bubbletea infrastructure
- Styles/colors hardcoded throughout render functions

### 4. **Reusability Issues**
- Can't reuse rendering logic outside TUI
- Can't swap views easily (e.g., different layouts for different screen sizes)
- Styles duplicated across files

---

## Proposed Architecture: MVVM Pattern

**Why MVVM over MVC?**
- Bubbletea's Update/View pattern naturally maps to MVVM
- Better separation of concerns for reactive UIs
- ViewModel can aggregate data from multiple sources
- Easier to test view logic without UI framework

```
┌─────────────────────────────────────────────────────────────┐
│                        Bubbletea App                        │
│  ┌──────────────────────────────────────────────────────┐  │
│  │                 Model (Core State)                    │  │
│  │  - Raw execution data (runs, logs, metrics)          │  │
│  │  - Event handling (messages)                         │  │
│  │  - State mutations                                   │  │
│  │  - Navigation state (page, pane, selection)         │  │
│  └────────────────┬─────────────────────────────────────┘  │
│                   │                                         │
│                   ▼                                         │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              ViewModels (Data Prep)                   │  │
│  │  - RunsTableViewModel                                │  │
│  │  - LogsViewModel                                     │  │
│  │  - SummaryViewModel                                  │  │
│  │  - ConversationViewModel                             │  │
│  │  → Transform Model data for display                  │  │
│  │  → Compute derived state                             │  │
│  │  → Format values                                     │  │
│  └────────────────┬─────────────────────────────────────┘  │
│                   │                                         │
│                   ▼                                         │
│  ┌──────────────────────────────────────────────────────┐  │
│  │               Views (Pure Rendering)                  │  │
│  │  - RunsTableView                                     │  │
│  │  - LogsPanelView                                     │  │
│  │  - SummaryView                                       │  │
│  │  - ConversationView                                  │  │
│  │  → Receive ViewModels                                │  │
│  │  → Return styled strings                             │  │
│  │  → No business logic                                 │  │
│  └────────────────┬─────────────────────────────────────┘  │
│                   │                                         │
│                   ▼                                         │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              Layouts (Composition)                    │  │
│  │  - MainLayout                                        │  │
│  │  - ConversationLayout                                │  │
│  │  → Compose views                                     │  │
│  │  → Handle responsive sizing                          │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘

Supporting Layers:
┌──────────────────────────────────────────────────────────┐
│                   Theme/Styles                            │
│  - Centralized color palette                             │
│  - Reusable style definitions                            │
│  - Dark/light theme support                              │
└──────────────────────────────────────────────────────────┘
```

---

## Proposed Directory Structure

```
tools/arena/tui/
├── tui.go                    # Main bubbletea integration (Model.Init/Update/View)
├── model.go                  # Core state and mutations (NEW - extracted)
├── messages.go              # Event messages (KEEP)
├── event_adapter.go         # Event-to-message adapter (KEEP)
├── log_interceptor.go       # Log capture (KEEP)
│
├── viewmodels/              # NEW LAYER
│   ├── runs_table_vm.go    # Transforms []RunInfo → table rows
│   ├── logs_vm.go          # Formats logs for display
│   ├── summary_vm.go       # Aggregates metrics
│   ├── conversation_vm.go  # Formats conversation turns
│   └── result_vm.go        # Formats run results
│
├── views/                   # NEW LAYER (Pure rendering)
│   ├── runs_table.go       # RunsTableView.Render(vm) → string
│   ├── logs_panel.go       # LogsPanelView.Render(vm) → string
│   ├── summary_panel.go    # SummaryView.Render(vm) → string
│   ├── conversation.go     # ConversationView.Render(vm) → string
│   ├── result_panel.go     # ResultView.Render(vm) → string
│   └── header_footer.go    # Header/FooterView.Render(vm) → string
│
├── layouts/                 # NEW LAYER (Composition)
│   ├── main_layout.go      # 4-panel layout
│   ├── conversation_layout.go  # Conversation-focused layout
│   └── responsive.go       # Size calculations
│
├── theme/                   # NEW LAYER (Styles)
│   ├── colors.go           # Color constants
│   ├── styles.go           # Reusable lipgloss styles
│   └── formatters.go       # Value formatting (duration, cost, etc)
│
├── state/                   # Domain logic (REFACTOR from model.go)
│   ├── runs.go             # RunInfo, RunStatus types
│   ├── logs.go             # LogEntry type
│   └── summary.go          # Summary type and builders
│
└── *_test.go               # Tests organized by layer
```

---

## Implementation Plan

### Phase 1: Extract Theme Layer (Low Risk)
**Goal**: Centralize styles and colors

```go
// theme/colors.go
package theme

var (
    ColorPrimary   = "#7C3AED"
    ColorSuccess   = "#10B981"
    ColorError     = "#EF4444"
    // ... all colors
)

// theme/styles.go
package theme

import "github.com/charmbracelet/lipgloss"

var (
    TitleStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color(ColorPrimary))
    
    BorderedBox = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        Padding(1, 2)
    
    // ... reusable styles
)

// theme/formatters.go
package theme

func FormatDuration(d time.Duration) string { /* ... */ }
func FormatCost(cost float64) string { /* ... */ }
func FormatNumber(n int64) string { /* ... */ }
```

**Migration**: Update existing render functions to use `theme.TitleStyle` instead of inline definitions.

---

### Phase 2: Create ViewModels (Medium Risk)
**Goal**: Separate data preparation from rendering

```go
// viewmodels/runs_table_vm.go
package viewmodels

type RunsTableViewModel struct {
    Rows          []RunTableRow
    SelectedIndex int
    IsFocused     bool
    Height        int
    Width         int
}

type RunTableRow struct {
    Status    string // Pre-formatted with style
    Provider  string
    Scenario  string
    Region    string
    Duration  string
    Cost      string
    Notes     string
    IsActive  bool
}

func NewRunsTableViewModel(
    runs []tui.RunInfo,
    selectedIdx int,
    focused bool,
    dimensions Dimensions,
) *RunsTableViewModel {
    rows := make([]RunTableRow, len(runs))
    for i, run := range runs {
        rows[i] = RunTableRow{
            Status:   formatRunStatus(run.Status),
            Provider: run.Provider,
            // ... format all fields
        }
    }
    return &RunsTableViewModel{
        Rows: rows,
        // ...
    }
}
```

**Migration**: Gradually move data transformation logic from render methods into ViewModel constructors.

---

### Phase 3: Create Pure Views (Medium Risk)
**Goal**: Render functions that only format strings

```go
// views/runs_table.go
package views

import "github.com/charmbracelet/bubbles/table"

type RunsTableView struct{}

func (v RunsTableView) Render(vm *viewmodels.RunsTableViewModel) string {
    // Pure rendering - no business logic
    // Receives pre-formatted data
    // Returns styled string
    
    t := table.New(
        table.WithColumns(v.columns(vm.Width)),
        table.WithRows(v.rows(vm)),
        table.WithHeight(vm.Height),
        table.WithFocused(vm.IsFocused),
    )
    
    t.SetStyles(theme.TableStyles)
    
    return theme.BorderedBox.
        BorderForeground(v.borderColor(vm.IsFocused)).
        Render(t.View())
}

func (v RunsTableView) rows(vm *viewmodels.RunsTableViewModel) []table.Row {
    rows := make([]table.Row, len(vm.Rows))
    for i, r := range vm.Rows {
        rows[i] = table.Row{r.Status, r.Provider, r.Scenario, ...}
    }
    return rows
}
```

**Migration**: Extract render functions into View structs, have them accept ViewModels.

---

### Phase 4: Create Layouts (Low Risk)
**Goal**: Compose views into full screens

```go
// layouts/main_layout.go
package layouts

type MainLayout struct {
    runsView    views.RunsTableView
    logsView    views.LogsPanelView
    resultView  views.ResultView
    summaryView views.SummaryView
}

func (l MainLayout) Render(
    runsVM *viewmodels.RunsTableViewModel,
    logsVM *viewmodels.LogsViewModel,
    resultVM *viewmodels.ResultViewModel,
    summaryVM *viewmodels.SummaryViewModel,
) string {
    top := lipgloss.JoinHorizontal(
        lipgloss.Top,
        l.runsView.Render(runsVM),
        l.resultView.Render(resultVM),
    )
    
    bottom := lipgloss.JoinHorizontal(
        lipgloss.Top,
        l.logsView.Render(logsVM),
        l.summaryView.Render(summaryVM),
    )
    
    return lipgloss.JoinVertical(lipgloss.Left, top, "", bottom)
}
```

---

### Phase 5: Refactor Model (Higher Risk)
**Goal**: Model only manages state, delegates to ViewModels and Views

```go
// tui.go
func (m *Model) View() string {
    m.mu.Lock()
    defer m.mu.Unlock()

    if !m.isTUIMode {
        return ""
    }

    switch m.currentPage {
    case pageMain:
        return m.renderMainPage()
    case pageConversation:
        return m.renderConversationPage()
    default:
        return "Loading..."
    }
}

func (m *Model) renderMainPage() string {
    // Create ViewModels (data preparation)
    runsVM := viewmodels.NewRunsTableViewModel(
        m.activeRuns,
        m.runsTable.Cursor(),
        m.activePane == paneRuns,
        viewmodels.Dimensions{Width: m.width, Height: m.height},
    )
    
    logsVM := viewmodels.NewLogsViewModel(
        m.logs,
        m.logViewport,
        m.activePane == paneLogs,
    )
    
    summaryVM := viewmodels.NewSummaryViewModel(
        m.buildSummary("", ""),
        m.width,
    )
    
    resultVM := m.buildResultViewModel()
    
    // Render layout
    layout := layouts.MainLayout{}
    body := layout.Render(runsVM, logsVM, resultVM, summaryVM)
    
    // Add header/footer
    header := views.HeaderView{}.Render(m.headerViewModel())
    footer := views.FooterView{}.Render(m.footerViewModel())
    
    return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
}
```

---

## Benefits

### ✅ Testability
```go
// Easy to test ViewModels without bubbletea
func TestRunsTableViewModel_FormatsStatus(t *testing.T) {
    runs := []RunInfo{{Status: StatusRunning}}
    vm := NewRunsTableViewModel(runs, 0, false, defaultDimensions)
    assert.Equal(t, "⏳ Running", vm.Rows[0].Status)
}

// Easy to test Views with mock data
func TestRunsTableView_Renders(t *testing.T) {
    vm := &RunsTableViewModel{Rows: []RunTableRow{{Status: "Test"}}}
    output := RunsTableView{}.Render(vm)
    assert.Contains(t, output, "Test")
}
```

### ✅ Reusability
- ViewModels can be used for non-TUI output (JSON, HTML export)
- Views can be swapped (minimal vs detailed modes)
- Layouts can adapt to screen sizes

### ✅ Maintainability
- Clear responsibilities: Model (state), ViewModel (transform), View (render)
- Changes to styling don't require touching business logic
- New views can be added without modifying core Model

### ✅ Type Safety
- ViewModels enforce data contracts between layers
- Compile-time checks for required data
- No stringly-typed data passing

---

## Migration Strategy

### Option A: Gradual (Recommended)
1. Extract theme layer (1-2 hours)
2. Create ViewModels for runs panel (2-3 hours)
3. Create corresponding View (1-2 hours)
4. Repeat for each panel (logs, summary, etc.)
5. Refactor layouts last (1-2 hours)

**Total**: ~2-3 days of work, spread over multiple PRs

### Option B: Big Bang (Risky)
- Rewrite entire architecture at once
- High risk of bugs
- Not recommended unless you freeze other development

---

## Alternative: Lighter Refactoring

If full MVVM is too heavy, consider:

### Minimal Separation
```
tui/
├── core/          # Model, events, state
├── presentation/  # All render logic
│   ├── panels/   # Individual panel renderers
│   ├── layouts/  # Composition
│   └── styles/   # Themes
└── adapters/      # Event adapters, interceptors
```

This gives you:
- ✅ Clear separation of concerns
- ✅ Easier testing
- ✅ Better organization
- ❌ Still some coupling between data and presentation
- ❌ Less flexibility for alternative UIs

---

## Recommendation

**Start with Phase 1 (Theme) + Phase 2 (ViewModels) for runs panel only.**

This gives you:
1. Immediate improvement in code organization
2. Proof of concept for the pattern
3. Low risk (theme extraction is straightforward)
4. Testable pattern to evaluate before committing

Then decide:
- If it feels good → continue with other panels
- If it's too complex → stick with lighter refactoring
- If it's overkill → stop and just use theme layer

---

## Code Sample: Before/After

### Before
```go
// runs_panel.go - All mixed together
func (m *Model) renderActiveRuns() string {
    // Data transformation
    rows := make([]table.Row, len(m.activeRuns))
    for i, run := range m.activeRuns {
        statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
        status := statusStyle.Render("✓ Done")
        duration := fmt.Sprintf("%.1fs", run.Duration.Seconds())
        cost := fmt.Sprintf("$%.4f", run.Cost)
        rows[i] = table.Row{status, run.Provider, duration, cost}
    }
    
    // Rendering
    t := table.New(table.WithRows(rows))
    borderStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
    return borderStyle.Render(t.View())
}
```

### After
```go
// viewmodels/runs_table_vm.go - Data prep
func NewRunsTableViewModel(runs []RunInfo, ...) *RunsTableViewModel {
    rows := make([]RunTableRow, len(runs))
    for i, run := range runs {
        rows[i] = RunTableRow{
            Status:   theme.FormatRunStatus(run.Status),
            Provider: run.Provider,
            Duration: theme.FormatDuration(run.Duration),
            Cost:     theme.FormatCost(run.Cost),
        }
    }
    return &RunsTableViewModel{Rows: rows}
}

// views/runs_table.go - Pure rendering
func (v RunsTableView) Render(vm *RunsTableViewModel) string {
    t := table.New(table.WithRows(v.toTableRows(vm.Rows)))
    return theme.BorderedBox.Render(t.View())
}

// tui.go - Composition
func (m *Model) View() string {
    vm := viewmodels.NewRunsTableViewModel(m.activeRuns, ...)
    return views.RunsTableView{}.Render(vm)
}
```

**Result**: Each layer has one responsibility, easily testable, maintainable.
