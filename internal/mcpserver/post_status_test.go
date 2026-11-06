// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func resultText(res *mcp.CallToolResult) string {
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func TestPostStatus(t *testing.T) {
	const listed = "31600000001@s.whatsapp.net"
	post := func(opts Options, args map[string]any) (*mcp.CallToolResult, *fakeWA) {
		t.Helper()
		fake := &fakeWA{}
		cs := connect(t, fake, storetest.New(t), opts)
		return call(t, cs, "post_status", args, nil), fake
	}
	text := map[string]any{"text": "  Back from holiday  "}

	// With an allowlist and no -allow-status: refused and logged, nothing sent.
	var logs bytes.Buffer
	allowlisted := Options{AllowRecipients: []string{listed}, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	if res, fake := post(allowlisted, text); !res.IsError || !strings.Contains(resultText(res), "-allow-status") || len(fake.sent) != 0 {
		t.Errorf("allowlist, flag off = %q, sent %v", resultText(res), fake.sent)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "post_status") {
		t.Errorf("refusal not logged: %q", logs.String())
	}
	// With the flag on, and without any allowlist, it posts.
	for name, opts := range map[string]Options{
		"allowlist, flag on": {AllowRecipients: []string{listed}, AllowStatus: true},
		"no allowlist":       {},
	} {
		res, fake := post(opts, text)
		if res.IsError || !slices.Equal(fake.sent, []string{`status "Back from holiday"`}) {
			t.Errorf("%s = %q, sent %v", name, resultText(res), fake.sent)
		}
	}

	// A photo goes through send_media's path to the status broadcast.
	img := base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xff\xe0 jpeg"))
	res, fake := post(Options{}, map[string]any{"kind": "image", "data": img, "caption": "view"})
	if res.IsError || len(fake.sent) != 1 || !strings.HasPrefix(fake.sent[0], "media status@broadcast image") {
		t.Errorf("photo status = %q, sent %v", resultText(res), fake.sent)
	}

	for name, args := range map[string]map[string]any{
		"nothing":         {},
		"blank text":      {"text": "   "},
		"text and a file": {"text": "hi", "kind": "image", "data": img},
		"a document":      {"kind": "document", "data": img},
		"no kind":         {"data": img},
		"data and path":   {"kind": "image", "data": img, "path": "a.jpg"},
	} {
		if res, fake := post(Options{}, args); !res.IsError || len(fake.sent) != 0 {
			t.Errorf("%s: post_status = %q, sent %v", name, resultText(res), fake.sent)
		}
	}
}

// Only post_status may post a status: the other write tools refuse the status
// broadcast even when the allowlist lists it, and without any allowlist,
// while post_status still posts.
func TestWriteToolsRefuseStatusBroadcast(t *testing.T) {
	const status = "status@broadcast"
	for name, opts := range map[string]Options{
		"listed":       {AllowRecipients: []string{status}, AllowStatus: true},
		"no allowlist": {},
	} {
		var logs bytes.Buffer
		opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
		fake := &fakeWA{}
		cs := connect(t, fake, storetest.New(t), opts)
		res := call(t, cs, "send_message", map[string]any{"recipient": status, "text": "hi"}, nil)
		if !res.IsError || !strings.Contains(resultText(res), "post_status") || len(fake.sent) != 0 {
			t.Errorf("%s: send_message = %q, sent %v", name, resultText(res), fake.sent)
		}
		if !strings.Contains(logs.String(), "level=WARN") {
			t.Errorf("%s: refusal not logged: %q", name, logs.String())
		}
		if res := call(t, cs, "post_status", map[string]any{"text": "hi"}, nil); res.IsError || len(fake.sent) != 1 {
			t.Errorf("%s: post_status = %q, sent %v", name, resultText(res), fake.sent)
		}
	}
}

// Your own status can be deleted under post_status's rule; someone else's
// can't, and edit_message still refuses the status broadcast.
func TestDeleteOwnStatus(t *testing.T) {
	const status = "status@broadcast"
	newStore := func(t *testing.T) *store.Store {
		t.Helper()
		st := storetest.New(t)
		if err := st.Save(context.Background(), nil, []store.Message{
			{ChatJID: status, MessageID: "MYSTATUS", SenderJID: "31600000000@s.whatsapp.net", FromMe: true, Timestamp: 1, Text: "oops"},
			{ChatJID: status, MessageID: "THEIRS", SenderJID: alice, Timestamp: 2, Text: "beach"},
		}); err != nil {
			t.Fatal(err)
		}
		return st
	}
	del := func(id string) map[string]any { return map[string]any{"chat_jid": status, "message_id": id} }
	for name, tt := range map[string]struct {
		opts Options
		ok   bool
	}{
		"no allowlist":        {Options{}, true},
		"allowlist, flag on":  {Options{AllowRecipients: []string{alice}, AllowStatus: true}, true},
		"allowlist, flag off": {Options{AllowRecipients: []string{alice}}, false},
	} {
		var logs bytes.Buffer
		tt.opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
		fake := &fakeWA{}
		cs := connect(t, fake, newStore(t), tt.opts)
		res := call(t, cs, "delete_message", del("MYSTATUS"), nil)
		want := []string{"delete status@broadcast/MYSTATUS"}
		if tt.ok && (res.IsError || !slices.Equal(fake.sent, want)) {
			t.Errorf("%s: delete own status = %q, sent %v", name, resultText(res), fake.sent)
		}
		if !tt.ok && (!res.IsError || !strings.Contains(resultText(res), "-allow-status") || len(fake.sent) != 0 ||
			!strings.Contains(logs.String(), "tool=delete_message")) {
			t.Errorf("%s: delete own status = %q, sent %v, logs %q", name, resultText(res), fake.sent, logs.String())
		}
		fake.sent = nil
		for tool, args := range map[string]map[string]any{
			"delete_message": del("THEIRS"),
			"edit_message":   {"chat_jid": status, "message_id": "MYSTATUS", "text": "fixed"},
		} {
			if res := call(t, cs, tool, args, nil); !res.IsError || len(fake.sent) != 0 {
				t.Errorf("%s: %s = %q, sent %v", name, tool, resultText(res), fake.sent)
			}
		}
	}
}
