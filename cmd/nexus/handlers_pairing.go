package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/pairing"
	"github.com/spf13/cobra"
)

// =============================================================================
// Pairing Command Handlers
// =============================================================================

var pairingProviders = []string{
	"telegram",
	"discord",
	"slack",
	"whatsapp",
	"signal",
	"imessage",
	"matrix",
	"teams",
	"mattermost",
	"nextcloud-talk",
	"zalo",
	"bluebubbles",
}

func runPairingList(cmd *cobra.Command, provider string) error {
	provider = normalizePairingProvider(provider)
	out := cmd.OutOrStdout()

	providers := pairingProviders
	if provider != "" {
		providers = []string{provider}
	}

	found := false
	for _, name := range providers {
		store := pairing.NewStore(name)
		pending, err := store.ListRequests(name)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			continue
		}
		found = true
		fmt.Fprintf(out, "%s:\n", name)
		for _, req := range pending {
			label := req.Meta["name"]
			if strings.TrimSpace(label) == "" {
				label = req.ID
			}
			expiresIn := time.Until(req.ExpiresAt).Round(time.Minute)
			if expiresIn < 0 {
				expiresIn = 0
			}
			fmt.Fprintf(out, "  %s  %s  expires in %s\n", req.Code, label, expiresIn)
		}
	}

	if !found {
		fmt.Fprintln(out, "No pending pairing requests.")
	}
	return nil
}

func runPairingApprove(cmd *cobra.Command, code string, provider string) error {
	return runPairingDecision(cmd, code, provider, true)
}

func runPairingDeny(cmd *cobra.Command, code string, provider string) error {
	return runPairingDecision(cmd, code, provider, false)
}

func runPairingDecision(cmd *cobra.Command, code string, provider string, approve bool) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("pairing code is required")
	}
	provider = normalizePairingProvider(provider)
	if provider == "" {
		match, err := findPairingProviderForCode(code)
		if err != nil {
			return err
		}
		provider = match
	}

	store := pairing.NewStore(provider)
	var req *pairing.Request
	var err error
	var id string
	if approve {
		id, req, err = store.ApproveCode(provider, code)
	} else {
		// Deny just removes from pending without adding to allowlist
		id, req, err = store.ApproveCode(provider, code)
		if err == nil {
			// Remove from allowlist since we're denying
			if removeErr := store.RemoveFromAllowlist(provider, id); removeErr != nil {
				return fmt.Errorf("remove from allowlist: %w", removeErr)
			}
		}
	}
	if err != nil {
		if errors.Is(err, pairing.ErrCodeNotFound) {
			return fmt.Errorf("pairing code %q not found", code)
		}
		return err
	}

	action := "Denied"
	if approve {
		action = "Approved"
	}
	label := ""
	if req != nil {
		label = req.Meta["name"]
		if strings.TrimSpace(label) == "" {
			label = req.ID
		}
	}
	if label == "" {
		label = id
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s for %s (%s).\n", action, code, provider, label)
	return nil
}

func normalizePairingProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func findPairingProviderForCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", fmt.Errorf("pairing code is required")
	}
	var match string
	for _, provider := range pairingProviders {
		store := pairing.NewStore(provider)
		pending, err := store.ListRequests(provider)
		if err != nil {
			return "", err
		}
		for _, req := range pending {
			if strings.EqualFold(req.Code, code) {
				if match != "" && match != provider {
					return "", fmt.Errorf("pairing code %q found in multiple providers; use --provider", code)
				}
				match = provider
			}
		}
	}
	if match == "" {
		return "", fmt.Errorf("pairing code %q not found", code)
	}
	return match, nil
}
