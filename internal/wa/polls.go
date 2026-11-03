// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// pollCreation returns the poll a message carries, whichever of the poll
// message versions it was sent as, or nil if it is not a poll.
func pollCreation(m *waE2E.Message) *waE2E.PollCreationMessage {
	for _, p := range []*waE2E.PollCreationMessage{
		m.GetPollCreationMessage(),
		m.GetPollCreationMessageV2(),
		m.GetPollCreationMessageV3(),
		m.GetPollCreationMessageV5(),
		m.GetPollCreationMessageV6(),
	} {
		if p != nil {
			return p
		}
	}
	// V4 wraps the poll in a future-proof message.
	if inner := m.GetPollCreationMessageV4().GetMessage(); inner != nil {
		return pollCreation(inner)
	}
	return nil
}

// pollText is the archived text of a poll: the question, then one line per
// option, so that both are found by search.
func pollText(p *waE2E.PollCreationMessage) string {
	var b strings.Builder
	b.WriteString(p.GetName())
	for _, o := range p.GetOptions() {
		b.WriteString("\n- ")
		b.WriteString(o.GetOptionName())
	}
	return b.String()
}
