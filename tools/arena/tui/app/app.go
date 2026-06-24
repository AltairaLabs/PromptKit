// Package app is declared in page.go; this file adds the App root model.
package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// App is the root bubbletea model for the PromptArena TUI hub shell.
// It owns the page navigation stack and routes messages globally.
type App struct {
	ctx       *AppContext
	stack     []Page
	w, h      int
	send      func(tea.Msg)
	activated map[Page]bool // pages whose Activate has already fired (idempotency)
	inited    map[Page]bool // pages whose Init has already been called (idempotency)
}

// SetSend stores the program's Send func so that Activatable pages can push
// messages back into the bubbletea event loop from goroutines. Call this after
// tea.NewProgram and before p.Run().
func (a *App) SetSend(send func(tea.Msg)) {
	a.send = send
}

// New creates a new App with root as the initial (bottom) page on the stack.
// root must not be nil.
func New(ctx *AppContext, root Page) *App {
	return &App{
		ctx:       ctx,
		stack:     []Page{root},
		activated: map[Page]bool{},
		inited:    map[Page]bool{},
	}
}

// Init implements tea.Model. It runs the top page's Init command (once only)
// and batches it with an optional Activate cmd if the page implements Activatable.
func (a *App) Init() tea.Cmd {
	return a.initAndActivate(a.top())
}

// Update implements tea.Model. It handles global navigation and key messages,
// forwarding everything else to the top page.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
		a.top().SetSize(a.w, a.h)
		return a, nil

	case PushPageMsg:
		cmd := a.push(m.Page)
		return a, cmd

	case PopPageMsg:
		cmd := a.pop()
		return a, cmd

	case QuitMsg:
		a.closeAll()
		return a, tea.Quit

	case ConfigChangedMsg:
		// config changed: drop any cached engine so it rebuilds against the new config
		a.ctx.Engine = nil
		return a, nil

	case tea.KeyMsg:
		//nolint:exhaustive // Only handling specific navigation and quit keys.
		switch m.Type {
		case tea.KeyCtrlC:
			a.closeAll()
			return a, tea.Quit
		case tea.KeyEsc:
			if !a.atRoot() {
				cmd := a.pop()
				return a, cmd
			}
			a.closeAll()
			return a, tea.Quit
		case tea.KeyRunes:
			if len(m.Runes) == 1 && m.Runes[0] == 'q' {
				a.closeAll()
				return a, tea.Quit
			}
		}
	}

	// Forward all other messages to the top page.
	newPage, cmd := a.top().Update(msg)
	a.stack[len(a.stack)-1] = newPage
	return a, cmd
}

// View implements tea.Model. It delegates to the top page.
func (a *App) View() string {
	return a.top().View()
}

// push pushes p onto the stack, calls SetSize, and returns its Init cmd (once only)
// batched with an optional Activate cmd if p implements Activatable.
func (a *App) push(p Page) tea.Cmd {
	p.SetSize(a.w, a.h)
	a.stack = append(a.stack, p)
	return a.initAndActivate(p)
}

// initAndActivate calls p.Init() (first time only) and batches it with an
// optional Activate cmd if p implements Activatable (also first time only).
// It is the single gate used by App.Init(), push(), and pop() so that Init
// fires exactly once regardless of how a page reaches the top of the stack.
//
// The once-only guarantee means:
//   - Revealed roots (e.g. ChatPage deep-link) get Init() on splash dismiss.
//   - Re-revealed pages (e.g. RunPage mid-run after popping ConversationViewPage)
//     do NOT get a second Init() call.
func (a *App) initAndActivate(p Page) tea.Cmd {
	if a.inited == nil {
		a.inited = map[Page]bool{}
	}
	// Activate wires a page's dependencies (ChatPage's engine, RunPage's event
	// bus + the send func) and MUST run before Init, which consumes them —
	// ChatPage.Init reads the engine that Activate sets. Running Init first left
	// it reading a nil engine, bailing, and falling through to its zero-value
	// state (an empty "Select an agent" picker). Neither page's Activate depends
	// on Init having run, so ordering Activate first is safe.
	activateCmd := a.activateIfNeeded(p)
	var initCmd tea.Cmd
	if !a.inited[p] {
		a.inited[p] = true
		initCmd = p.Init()
	}
	return tea.Batch(activateCmd, initCmd)
}

// activateIfNeeded calls Activate on p if it implements Activatable and has not
// already been activated, returning the resulting tea.Cmd. It never passes a
// nil send to Activate — if a.send has not been set (headless/test), a no-op
// func is used instead. Activation is tracked so a page is activated at most
// once even when it is revealed again by a pop.
func (a *App) activateIfNeeded(p Page) tea.Cmd {
	act, ok := p.(Activatable)
	if !ok {
		return nil
	}
	if a.activated == nil {
		a.activated = map[Page]bool{}
	}
	if a.activated[p] {
		return nil
	}
	a.activated[p] = true
	send := a.send
	if send == nil {
		send = func(tea.Msg) {}
	}
	return act.Activate(send)
}

// pop removes the top page from the stack and returns Init+Activate for the
// revealed page (Init runs once only — it will not re-Init a page that was already
// inited, so popping back to a mid-run RunPage is safe). It is a no-op when at root.
func (a *App) pop() tea.Cmd {
	if a.atRoot() {
		return nil
	}
	popped := a.top()
	a.stack = a.stack[:len(a.stack)-1]
	// Remove the popped page from tracking maps so it does not leak for the
	// lifetime of the App (M3).
	delete(a.activated, popped)
	delete(a.inited, popped)
	revealed := a.top()
	revealed.SetSize(a.w, a.h)
	return a.initAndActivate(revealed)
}

// atRoot reports whether the navigation stack has only one page (the root).
func (a *App) atRoot() bool {
	return len(a.stack) == 1
}

// top returns the current top-of-stack page.
func (a *App) top() Page {
	return a.stack[len(a.stack)-1]
}

// closeAll calls Close on every page in the stack that implements Closeable.
// This is invoked before returning tea.Quit so background goroutines (e.g. the
// voice driver) are signaled to stop before the process exits.
func (a *App) closeAll() {
	for _, p := range a.stack {
		if c, ok := p.(Closeable); ok {
			c.Close()
		}
	}
}
