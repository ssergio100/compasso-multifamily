package main

import (
	"context"
	"os/user"
	"strings"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// WindowsUser returns the account shown by the settings screen.
func (a *App) WindowsUser() string {
	current, err := user.Current()
	if err != nil {
		return "Conta atual"
	}
	parts := strings.Split(current.Username, `\`)
	return parts[len(parts)-1]
}
