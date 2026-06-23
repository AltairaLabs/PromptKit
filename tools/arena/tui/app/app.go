// Package app is declared in page.go; this file adds the App root model.
package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// App is the root bubbletea model for the PromptArena TUI hub shell.
// It owns the page navigation stack and routes messages globally.
type App struct {
	ctx   *AppContext
	stack []Page
	w, h  int
	send  func(tea.Msg)
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
		ctx:   ctx,
		stack: []Page{root},
	}
}

// Init implements tea.Model. It runs the top page's Init command. If the top
// page also implements Activatable, its Activate cmd is batched with Init.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.top().Init(), a.activateIfNeeded(a.top()))
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
		a.pop()
		return a, nil

	case QuitMsg:
		return a, tea.Quit

	case ConfigChangedMsg:
		// config changed: drop any cached engine so it rebuilds against the new config
		a.ctx.Engine = nil
		return a, nil

	case tea.KeyMsg:
		//nolint:exhaustive // Only handling specific navigation and quit keys.
		switch m.Type {
		case tea.KeyCtrlC:
			return a, tea.Quit
		case tea.KeyEsc:
			if !a.atRoot() {
				a.pop()
				return a, nil
			}
			return a, tea.Quit
		case tea.KeyRunes:
			if len(m.Runes) == 1 && m.Runes[0] == 'q' {
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

// push pushes p onto the stack, calls SetSize, and returns its Init cmd
// batched with an optional Activate cmd if p implements Activatable.
func (a *App) push(p Page) tea.Cmd {
	p.SetSize(a.w, a.h)
	a.stack = append(a.stack, p)
	return tea.Batch(p.Init(), a.activateIfNeeded(p))
}

// activateIfNeeded calls Activate on p if it implements Activatable and returns
// the resulting tea.Cmd. It never passes a nil send to Activate — if a.send has
// not been set (headless/test), a no-op func is used instead.
func (a *App) activateIfNeeded(p Page) tea.Cmd {
	act, ok := p.(Activatable)
	if !ok {
		return nil
	}
	send := a.send
	if send == nil {
		send = func(tea.Msg) {}
	}
	return act.Activate(send)
}

// pop removes the top page from the stack. It is a no-op when at root.
func (a *App) pop() {
	if a.atRoot() {
		return
	}
	a.stack = a.stack[:len(a.stack)-1]
	a.top().SetSize(a.w, a.h)
}

// atRoot reports whether the navigation stack has only one page (the root).
func (a *App) atRoot() bool {
	return len(a.stack) == 1
}

// top returns the current top-of-stack page.
func (a *App) top() Page {
	return a.stack[len(a.stack)-1]
}
