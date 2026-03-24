// Package plugin provides a Nukkit-style plugin system for bedrock-dev.
//
// Plugins register themselves via their init() function calling Register().
// The server then calls EnableAll() after startup and DisableAll() on shutdown.
//
// Example plugin:
//
//	package myplugin
//
//	import "github.com/overmind/bedrock-dev/plugin"
//
//	func init() { plugin.Register(&MyPlugin{}) }
//
//	type MyPlugin struct{ plugin.Base }
//	func (p *MyPlugin) Name()     string { return "MyPlugin" }
//	func (p *MyPlugin) Version()  string { return "1.0.0" }
//	func (p *MyPlugin) OnEnable() { p.Logger().Info("MyPlugin enabled") }
package plugin

import "log/slog"

// Plugin is the interface every bedrock-dev plugin must implement.
type Plugin interface {
	// Name returns the plugin's unique display name.
	Name() string
	// Version returns the plugin's version string.
	Version() string
	// OnLoad is called once when the plugin is registered, before OnEnable.
	OnLoad()
	// OnEnable is called when the server enables the plugin.
	OnEnable()
	// OnDisable is called when the server disables the plugin (e.g. on shutdown).
	OnDisable()
	// Logger returns a scoped logger for this plugin.
	Logger() *slog.Logger

	// internal: called by the manager
	setLogger(l *slog.Logger)
}

// Base is an embeddable struct that provides no-op default implementations
// for OnLoad, OnEnable, and OnDisable, and handles the logger.
// Embed it in your plugin struct and override only the methods you need.
type Base struct {
	log *slog.Logger
}

func (b *Base) OnLoad()    {}
func (b *Base) OnEnable()  {}
func (b *Base) OnDisable() {}

// Logger returns the plugin-scoped logger assigned by the manager.
// Returns a default logger if called before the plugin is registered.
func (b *Base) Logger() *slog.Logger {
	if b.log == nil {
		return slog.Default()
	}
	return b.log
}

func (b *Base) setLogger(l *slog.Logger) { b.log = l }
