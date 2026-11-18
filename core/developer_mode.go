// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"errors"

	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/pkg/logger"
)

// Developer mode lets an administrator install extension packages that no
// trusted market signed (D1 4.4). It is off by default. An administrator
// turns it on through its own admin endpoint, which asks for the
// extension.developer_mode capability and the account's password; the
// choice is stored in the extension.OptionDeveloperMode option. Setting
// [market] lock_developer_mode in config.toml keeps it off whatever the
// option says, for production sites. Package checks are never relaxed: in
// developer mode only the signature requirement is lifted.

// ErrDeveloperModeLocked reports an attempt to turn developer mode on while
// config.toml locks it.
var ErrDeveloperModeLocked = errors.New("developer mode is locked by [market] lock_developer_mode in config.toml")

// DeveloperModeState is whether developer mode is on, and why.
type DeveloperModeState struct {
	// Enabled is the effective state: requested and not locked.
	Enabled bool
	// Requested reports that the option is on.
	Requested bool
	// Locked reports [market] lock_developer_mode.
	Locked bool
}

// DeveloperMode returns the developer mode state.
func (e *Engine) DeveloperMode() DeveloperModeState {
	var state DeveloperModeState
	if e.Config != nil {
		state.Locked = e.Config.Market.LockDeveloperMode
	}
	if e.Options != nil {
		state.Requested = e.Options.Get(extension.OptionDeveloperMode) == "1"
	}
	state.Enabled = state.Requested && !state.Locked
	return state
}

// SetDeveloperMode turns developer mode on or off. Turning it on fails with
// ErrDeveloperModeLocked while config.toml locks it; turning it off always
// works. Callers check the capability and the password first.
func (e *Engine) SetDeveloperMode(on bool) error {
	if e.Options == nil {
		return errors.New("developer mode: options are not available")
	}
	if on && e.DeveloperMode().Locked {
		return ErrDeveloperModeLocked
	}
	value := "0"
	if on {
		value = "1"
	}
	if err := e.Options.Set(extension.OptionDeveloperMode, value); err != nil {
		return err
	}
	if on {
		logger.Warn("Developer mode turned on: packages without a valid market signature can be installed")
	} else {
		logger.Warn("Developer mode turned off")
	}
	return nil
}

// logDeveloperMode records the developer mode state at startup.
func (e *Engine) logDeveloperMode() {
	state := e.DeveloperMode()
	switch {
	case state.Locked:
		logger.Info("Developer mode is locked off by [market] lock_developer_mode", "requested", state.Requested)
	case state.Enabled:
		logger.Warn("Developer mode is on: packages without a valid market signature can be installed")
	}
}
