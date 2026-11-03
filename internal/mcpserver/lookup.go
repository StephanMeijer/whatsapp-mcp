// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type SearchContactsIn struct {
	Query string `json:"query" jsonschema:"case-insensitive substring of name, push name or phone number"`
}

type SearchContactsOut struct {
	Contacts []wa.Contact `json:"contacts"`
}

func (t *tools) searchContacts(ctx context.Context, _ *mcp.CallToolRequest, in SearchContactsIn) (*mcp.CallToolResult, SearchContactsOut, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, SearchContactsOut{}, errors.New("query is required")
	}
	contacts, err := t.wa.Contacts(ctx, in.Query)
	if err != nil {
		return nil, SearchContactsOut{}, err
	}
	return nil, SearchContactsOut{Contacts: contacts}, nil
}

// checkNumberMax caps a lookup: many numbers at once look like enumeration.
const checkNumberMax = 10

type CheckNumberIn struct {
	Phones []string `json:"phones" jsonschema:"numbers with a country code, such as +31612345678 or 0031612345678 (at most 10)"`
}

type CheckNumberOut struct {
	Numbers []wa.NumberCheck `json:"numbers" jsonschema:"one entry per distinct number, in the order given"`
}

// checkNumber sends nothing, but it does reach WhatsApp and reveals who is
// registered. So every input is validated before the lookup, and when an
// allowlist is set only the numbers on it (and the account's own) may be
// looked up: the list is the operator's limit on whom the agent deals with.
func (t *tools) checkNumber(ctx context.Context, _ *mcp.CallToolRequest, in CheckNumberIn) (*mcp.CallToolResult, CheckNumberOut, error) {
	if len(in.Phones) == 0 {
		return nil, CheckNumberOut{}, errors.New("phones is required")
	}
	if len(in.Phones) > checkNumberMax {
		return nil, CheckNumberOut{}, fmt.Errorf("at most %d numbers per call", checkNumberMax)
	}
	phones := make([]string, 0, len(in.Phones))
	for _, p := range in.Phones {
		n, err := wa.NormalizePhone(p)
		if err != nil {
			return nil, CheckNumberOut{}, err
		}
		if _, err := t.policy.checkRecipient(ctx, t.wa, n, t.wa.Status().JID); err != nil {
			return nil, CheckNumberOut{}, err
		}
		if !slices.Contains(phones, n) {
			phones = append(phones, n)
		}
	}
	checks, err := t.wa.CheckNumbers(ctx, phones)
	if err != nil {
		return nil, CheckNumberOut{}, err
	}
	return nil, CheckNumberOut{Numbers: checks}, nil
}
