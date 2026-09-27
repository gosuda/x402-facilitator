package nano

import (
	"context"
	"strings"
)

// VerificationResult is the outcome of consulting MinIndependentEndpoints
// independent RPC nodes for a block.
type VerificationResult struct {
	OK             bool
	ConfirmedSends int
	Consulted      int
	Reason         string
	Payer          string
}

// VerifyBlock consults at least MinIndependentEndpoints independent RPC
// endpoints and returns whether the block is a confirmed send that pays
// exactly the required raw amount to payTo. It FAILS CLOSED: any endpoint
// error, unconfirmed, unsupported subtype, wrong amount or wrong destination
// makes the whole verification fail, and fewer than MinIndependentEndpoints
// healthy endpoints is a refusal rather than a pass. payTo is compared
// case-insensitively (Nano addresses are checksum-heterogeneous but
// case-insensitive in account names); amount is compared as an exact raw
// string, ignoring surrounding whitespace.
func VerifyBlock(
	ctx context.Context,
	client *Client,
	endpoints []string,
	blockHash, payTo, amountRaw string,
) VerificationResult {
	active := make([]string, 0, len(endpoints))
	seen := make(map[string]bool)
	for _, e := range endpoints {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		active = append(active, e)
	}
	if len(active) < MinIndependentEndpoints {
		return VerificationResult{
			OK:        false,
			Consulted: len(active),
			Reason:    ErrInsufficientEndpoints.Error(),
		}
	}

	payTo = strings.ToLower(strings.TrimSpace(payTo))
	amountRaw = strings.TrimSpace(amountRaw)
	confirmed := 0
	consulted := 0
	payer := ""
	for _, e := range active {
		consulted++
		bf, err := client.BlockInfo(ctx, e, blockHash)
		if err != nil {
			return VerificationResult{OK: false, Consulted: consulted, Reason: err.Error()}
		}
		if bf.Subtype != "send" {
			return VerificationResult{OK: false, Consulted: consulted, Reason: "block is not a send: " + bf.Subtype}
		}
		if !bf.Confirmed {
			return VerificationResult{OK: false, Consulted: consulted, Reason: "block is not confirmed"}
		}
		if strings.ToLower(strings.TrimSpace(bf.Destination)) != payTo {
			return VerificationResult{OK: false, Consulted: consulted, Reason: "block destination does not match pay-to"}
		}
		if strings.TrimSpace(bf.AmountRaw) != amountRaw {
			return VerificationResult{OK: false, Consulted: consulted, Reason: "block amount does not match required amount"}
		}
		if payer == "" {
			payer = bf.Account
		}
		confirmed++
	}

	// Every consulted endpoint agreed: all must be confirmed sends matching
	// amount and destination. If any endpoint disagreed we already returned.
	if len(active)-confirmed != 0 || confirmed < MinIndependentEndpoints {
		return VerificationResult{OK: false, Consulted: consulted, Reason: "insufficient confirming endpoints"}
	}
	return VerificationResult{OK: true, ConfirmedSends: confirmed, Consulted: consulted, Payer: payer}
}

// IsBlockHash reports whether s is a valid 64-character hexadecimal Nano block
// hash.
func IsBlockHash(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
