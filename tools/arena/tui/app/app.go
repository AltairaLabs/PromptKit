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
}

// New creates a new App with root as the initial (bottom) page on the stack.
// root must not be nil.
func New(ctx *AppContext, root Page) *App {
	return &App{
		ctx:   ctx,
		stack: []Page{root},
	}
}

// Init implements tea.Model. It runs the top page's Init command.
func (a *App) Init() tea.Cmd {
	return a.top().Init()
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

// push pushes p onto the stack, calls SetSize, and returns its Init cmd.
func (a *App) push(p Page) tea.Cmd {
	p.SetSize(a.w, a.h)
	a.stack = append(a.stack, p)
	return p.Init()
}

// pop removes the top page from the stack. It is a no-op when at root.
func (a *App) pop() {
	if a.atRoot() {
		return
	}
	a.stack = a.stack[:len(a.stack)-1]
}

// atRoot reports whether the navigation stack has only one page (the root).
func (a *App) atRoot() bool {
	return len(a.stack) == 1
}

// top returns the current top-of-stack page.
func (a *App) top() Page {
	return a.stack[len(a.stack)-1]
}
