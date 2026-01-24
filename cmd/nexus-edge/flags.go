package main

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
)

func applyFlagOverrides(cmd *cobra.Command, base Config, flags Config, pairToken string) (Config, error) {
	tokenChanged := flagChanged(cmd, "token")
	pairChanged := flagChanged(cmd, "pair-token")
	if tokenChanged && pairChanged {
		return base, errors.New("use only one of --token or --pair-token")
	}

	if flagChanged(cmd, "core-url") {
		base.CoreURL = flags.CoreURL
	}
	if flagChanged(cmd, "edge-id") {
		base.EdgeID = flags.EdgeID
	}
	if flagChanged(cmd, "name") {
		base.Name = flags.Name
	}
	if flagChanged(cmd, "reconnect-delay") {
		base.ReconnectDelay = flags.ReconnectDelay
	}
	if flagChanged(cmd, "heartbeat-interval") {
		base.HeartbeatInterval = flags.HeartbeatInterval
	}
	if flagChanged(cmd, "log-level") {
		base.LogLevel = flags.LogLevel
	}
	if flagChanged(cmd, "channels") {
		base.ChannelTypes = flags.ChannelTypes
	}
	if tokenChanged {
		base.AuthToken = flags.AuthToken
		if strings.TrimSpace(base.PairingToken) == "" {
			base.PairingToken = flags.AuthToken
		}
	}
	if pairChanged {
		base.AuthToken = pairToken
		base.PairingToken = pairToken
	}

	return base, nil
}

func flagChanged(cmd *cobra.Command, name string) bool {
	if f := cmd.Flags().Lookup(name); f != nil {
		return f.Changed
	}
	if f := cmd.InheritedFlags().Lookup(name); f != nil {
		return f.Changed
	}
	return false
}
