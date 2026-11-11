// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestReact(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	fake.groups[testGroup] = &types.GroupInfo{JID: testGroup, AddressingMode: types.AddressingModePN}
	target := incoming(testGroup, alicePN, "A1", 100, "hi")
	if err := c.React(ctx, testGroup, target, "👍"); err != nil {
		t.Fatal(err)
	}
	r := fake.sent[0].msg.GetReactionMessage()
	if fake.sent[0].to != testGroup || r.GetText() != "👍" || r.GetKey().GetID() != "A1" || r.GetKey().GetParticipant() != alicePN.String() {
		t.Errorf("sent %v to %s", r, fake.sent[0].to)
	}
	fake.sendErr = errors.New("offline")
	if err := c.React(ctx, testGroup, target, ""); err == nil || !strings.Contains(err.Error(), "react:") {
		t.Errorf("err = %v", err)
	}
	fake.loggedIn = false
	if err := c.React(ctx, testGroup, target, "👍"); err == nil {
		t.Error("logged out: want an error")
	}
}

func TestDeleteMessage(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	theirs := incoming(alicePN, alicePN, "A1", 100, "hi")
	mine := store.Message{ChatJID: alicePN.String(), MessageID: "O1", SenderJID: ownJID.ToNonAD().String(), Timestamp: 101, FromMe: true, Text: "oops"}
	archive(t, c, theirs, mine)

	if err := c.DeleteMessage(ctx, alicePN, theirs); err == nil || len(fake.sent) != 0 {
		t.Errorf("deleting someone else's message: err %v, %d sent", err, len(fake.sent))
	}
	if err := c.DeleteMessage(ctx, alicePN, mine); err != nil {
		t.Fatal(err)
	}
	pm := fake.sent[0].msg.GetProtocolMessage()
	if pm.GetType() != waE2E.ProtocolMessage_REVOKE || pm.GetKey().GetID() != "O1" || !pm.GetKey().GetFromMe() {
		t.Errorf("sent %v, want a revoke of O1", pm)
	}
	if _, err := c.store.MessageByID(ctx, alicePN.String(), "O1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleted message still found: %v", err)
	}

	fake.sendErr = errors.New("offline")
	if err := c.DeleteMessage(ctx, alicePN, mine); err == nil || !strings.Contains(err.Error(), "delete message:") {
		t.Errorf("err = %v", err)
	}
	fake.loggedIn = false
	if err := c.DeleteMessage(ctx, alicePN, mine); err == nil {
		t.Error("logged out: want an error")
	}
}

func TestEditMessage(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	mine := store.Message{ChatJID: alicePN.String(), MessageID: "O1", SenderJID: ownJID.ToNonAD().String(),
		Timestamp: time.Now().Add(-time.Minute).Unix(), FromMe: true, Text: "teh"}
	archive(t, c, mine)

	fake.sendErr = errors.New("offline")
	if _, err := c.EditMessage(ctx, alicePN, mine, "the"); err == nil || !strings.Contains(err.Error(), "edit message:") {
		t.Errorf("err = %v", err)
	}
	if got := archived(t, c, alicePN, "O1"); got.Text != "teh" || got.EditedAt != 0 {
		t.Errorf("archive changed by a failed edit: %+v", got)
	}

	fake.sendErr = nil
	edited, err := c.EditMessage(ctx, alicePN, mine, "the")
	if err != nil {
		t.Fatal(err)
	}
	pm := fake.sent[0].msg.GetEditedMessage().GetMessage().GetProtocolMessage()
	if pm.GetType() != waE2E.ProtocolMessage_MESSAGE_EDIT || pm.GetKey().GetID() != "O1" || pm.GetEditedMessage().GetConversation() != "the" {
		t.Errorf("sent %v, want an edit of O1 to %q", pm, "the")
	}
	got := archived(t, c, alicePN, "O1")
	if got.Text != "the" || got.EditedAt == 0 || edited.Text != "the" {
		t.Errorf("archived %+v, returned %+v", got, edited)
	}

	if _, err := c.EditMessage(ctx, alicePN, incoming(alicePN, alicePN, "A1", time.Now().Unix(), "x"), "y"); err == nil {
		t.Error("editing someone else's message: want an error")
	}
	fake.loggedIn = false
	if _, err := c.EditMessage(ctx, alicePN, mine, "x"); err == nil {
		t.Error("logged out: want an error")
	}
}

func TestSendMedia(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)
	fake.upload = whatsmeow.UploadResponse{URL: "https://mmg.example/u", DirectPath: "/v/u", FileLength: uint64(len(png))}

	m, err := c.SendMedia(ctx, alicePN.String(), OutgoingMedia{Kind: "image", Data: png, Caption: "look"})
	if err != nil {
		t.Fatal(err)
	}
	img := fake.sent[0].msg.GetImageMessage()
	if !reflect.DeepEqual(fake.uploads, []whatsmeow.MediaType{whatsmeow.MediaImage}) ||
		img.GetURL() != "https://mmg.example/u" || img.GetMimetype() != "image/png" || img.GetCaption() != "look" {
		t.Errorf("uploads %v, sent %v", fake.uploads, img)
	}
	got := archived(t, c, alicePN, m.MessageID)
	if got.MediaType != "image" || got.Text != "look" || len(got.MediaRef) == 0 || !got.FromMe {
		t.Errorf("archived %+v", got)
	}

	for _, tt := range []struct {
		name string
		f    OutgoingMedia
		set  func(*fakeWA)
		want string
	}{
		{"empty file", OutgoingMedia{Kind: "image"}, nil, "empty"},
		{"wrong type", OutgoingMedia{Kind: "video", Data: png}, nil, "must be one of"},
		{"upload fails", OutgoingMedia{Kind: "image", Data: png}, func(f *fakeWA) { f.uploadErr = errors.New("offline") }, "upload:"},
		{"send fails", OutgoingMedia{Kind: "image", Data: png}, func(f *fakeWA) { f.sendErr = errors.New("offline") }, "send:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, fake, _, _ := newTestClient(t)
			if tt.set != nil {
				tt.set(fake)
			}
			if _, err := c.SendMedia(ctx, alicePN.String(), tt.f); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one containing %q", err, tt.want)
			}
			if len(fake.sent) != 0 {
				t.Errorf("%d messages sent", len(fake.sent))
			}
		})
	}

	fake.loggedIn = false
	if _, err := c.SendMedia(ctx, alicePN.String(), OutgoingMedia{Kind: "image", Data: png}); err == nil {
		t.Error("logged out: want an error")
	}
}

func TestMediaTypeFor(t *testing.T) {
	for kind, want := range map[store.MediaKind]whatsmeow.MediaType{
		store.MediaImage: whatsmeow.MediaImage, store.MediaVideo: whatsmeow.MediaVideo,
		store.MediaAudio: whatsmeow.MediaAudio, store.MediaDocument: whatsmeow.MediaDocument,
	} {
		if got := mediaTypeFor(kind); got != want {
			t.Errorf("mediaTypeFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestClientMedia(t *testing.T) {
	ctx := context.Background()
	ref := mediaRef(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		DirectPath: proto.String("/v/img"), Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(4),
	}}, false)
	m := store.Message{MessageID: "P1", MediaType: "image", MediaRef: ref}

	c, fake, _, _ := newTestClient(t)
	fake.download = []byte("data")
	got, err := c.Media(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "image" || got.MIMEType != "image/jpeg" || string(got.Data) != "data" {
		t.Errorf("Media = %+v", got)
	}
	// The ref's FileLength is the sender's claim; the real file decides.
	fake.download = make([]byte, MaxMediaBytes+1)
	if _, err := c.Media(ctx, m); !errors.Is(err, errTooLarge) {
		t.Errorf("lying FileLength: err = %v", err)
	}
	fake.download = make([]byte, MaxMediaBytes+mediaEncOverhead+1)
	if _, err := c.Media(ctx, m); !errors.Is(err, errTooLarge) {
		t.Errorf("capped write: err = %v", err)
	}
	fake.downloadErr = whatsmeow.ErrMediaDownloadFailedWith404
	if _, err := c.Media(ctx, m); err == nil || !strings.Contains(err.Error(), "no longer on WhatsApp's servers") {
		t.Errorf("gone: err = %v", err)
	}
	fake.downloadErr = errors.New("reset")
	if _, err := c.Media(ctx, m); err == nil || !strings.Contains(err.Error(), "download media") {
		t.Errorf("err = %v", err)
	}
	if _, err := c.Media(ctx, store.Message{MediaType: "image"}); !errors.Is(err, ErrNoMediaRef) {
		t.Errorf("no ref: err = %v", err)
	}
}

func TestCheckNumbers(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	fake.onWhatsApp = []types.IsOnWhatsAppResponse{{Query: "+31600000001", JID: alicePN, IsIn: true}}
	got, err := c.CheckNumbers(ctx, []string{"+31600000001", "+31600000005"})
	if err != nil {
		t.Fatal(err)
	}
	want := []NumberCheck{{Phone: "+31600000001", OnWhatsApp: true, JID: alicePN.String()}, {Phone: "+31600000005"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CheckNumbers = %+v, want %+v", got, want)
	}
	fake.lookupErr = errors.New("offline")
	if _, err := c.CheckNumbers(ctx, []string{"+31600000001"}); err == nil || !strings.Contains(err.Error(), "look up numbers") {
		t.Errorf("err = %v", err)
	}
	fake.loggedIn = false
	if _, err := c.CheckNumbers(ctx, []string{"+31600000001"}); err == nil {
		t.Error("logged out: want an error")
	}
}

func liveMessage(chat, sender types.JID, id string, fromMe bool, pushName string, m *waE2E.Message) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsFromMe: fromMe, IsGroup: chat.Server == types.GroupServer || chat.Server == types.BroadcastServer},
			ID:            id, PushName: pushName, Timestamp: time.Unix(100, 0),
		},
		Message: m,
	}
}

func TestOnMessage(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	aliceDevice := types.JID{User: aliceLID.User, Device: 3, Server: types.HiddenUserServer}

	// From a LID: archived under the number, and the chat named after the sender.
	c.onMessage(liveMessage(aliceLID, aliceDevice, "M1", false, "Alice P", text("hi")))
	if m := archived(t, c, alicePN, "M1"); m.SenderJID != alicePN.String() || m.Text != "hi" {
		t.Errorf("M1 = %+v", m)
	}
	// The account's own message carries its own push name, not the chat's.
	c.onMessage(liveMessage(bobPN, ownJID, "O1", true, "Me", text("yo")))
	if names := chatNames(t, c, alicePN, bobPN); names[alicePN.String()] != "Alice P" || names[bobPN.String()] != "" {
		t.Errorf("chat names = %v", names)
	}
	// Reactions are not archived.
	c.onMessage(liveMessage(alicePN, alicePN, "R1", false, "", &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}}))
	if _, err := c.store.MessageByID(context.Background(), alicePN.String(), "R1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("reaction archived: %v", err)
	}
	// A live edit is applied to the archived original.
	edit := liveMessage(aliceLID, aliceDevice, "E1", false, "", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:           &waCommon.MessageKey{ID: proto.String("M1")},
		EditedMessage: text("hi there"),
		TimestampMS:   proto.Int64(300_000),
	}})
	edit.IsEdit = true
	c.onMessage(edit)
	if m := archived(t, c, alicePN, "M1"); m.Text != "hi there" || m.EditedAt != 300 {
		t.Errorf("after edit M1 = %q at %d", m.Text, m.EditedAt)
	}
	// In a group, the chat is not named after the sender. The group-name
	// lookup is marked done first, so no background lookup starts.
	c.groupLookups.Store(testGroup.String(), struct{}{})
	c.onMessage(liveMessage(testGroup, alicePN, "G1", false, "Alice P", text("all")))
	archived(t, c, testGroup, "G1")
	if got := chatNames(t, c, testGroup)[testGroup.String()]; got != "" {
		t.Errorf("group named %q after a member", got)
	}
}

func TestOnGroupEvents(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	other := types.NewJID("120363000000000009", types.GroupServer)
	c.onJoinedGroup(&events.JoinedGroup{GroupInfo: types.GroupInfo{JID: testGroup, GroupName: types.GroupName{Name: "Family"}}})
	c.onJoinedGroup(&events.JoinedGroup{GroupInfo: types.GroupInfo{JID: other}}) // no name: nothing recorded
	c.onGroupInfo(&events.GroupInfo{JID: testGroup, Name: &types.GroupName{Name: "Family 2"}})
	c.onGroupInfo(&events.GroupInfo{JID: testGroup}) // no rename
	want := map[string]string{testGroup.String(): "Family 2"}
	if got := chatNames(t, c, testGroup, other); !reflect.DeepEqual(got, want) {
		t.Errorf("chats = %v, want %v", got, want)
	}
}

func TestCalls(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	aliceDevice := types.JID{User: aliceLID.User, Device: 3, Server: types.HiddenUserServer}
	offer := func(id string) {
		c.onCallEvent(&events.CallOffer{BasicCallMeta: types.BasicCallMeta{From: aliceDevice, CallCreator: aliceDevice, CallID: id, Timestamp: time.Unix(100, 0)}})
	}
	offer("C1")
	offer("C2")
	call, err := c.store.CallByID(ctx, "C1")
	if err != nil || call.Status != store.CallRinging || call.CallerJID != alicePN.String() {
		t.Fatalf("C1 = %+v, err %v; want ringing from %s", call, err, alicePN)
	}

	rejected, err := c.RejectCall(ctx, "C1")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != store.CallRejected || !reflect.DeepEqual(fake.rejected, []rejectCall{{aliceLID, "C1"}}) {
		t.Errorf("rejected %+v, calls %+v", rejected, fake.rejected)
	}
	if call, _ := c.store.CallByID(ctx, "C1"); call.Status != store.CallRejected {
		t.Errorf("archived C1 status = %q", call.Status)
	}
	if _, err := c.RejectCall(ctx, "C1"); err == nil || len(fake.rejected) != 1 {
		t.Errorf("rejecting again: err %v, %d calls", err, len(fake.rejected))
	}

	c.onCallEvent(&events.CallTerminate{BasicCallMeta: types.BasicCallMeta{CallID: "C2", Timestamp: time.Unix(110, 0)}, Reason: "rejected_elsewhere"})
	if call, _ := c.store.CallByID(ctx, "C2"); call.Status != store.CallRejected {
		t.Errorf("C2 status = %q, want rejected", call.Status)
	}

	offer("C3")
	fake.rejectErr = errors.New("offline")
	if _, err := c.RejectCall(ctx, "C3"); err == nil || !strings.Contains(err.Error(), "reject call:") {
		t.Errorf("err = %v", err)
	}
	fake.loggedIn = false
	if _, err := c.RejectCall(ctx, "C3"); err == nil {
		t.Error("logged out: want an error")
	}
}

func TestFetchOlder(t *testing.T) {
	ctx := context.Background()
	c, fake, _, _ := newTestClient(t)
	if _, err := c.FetchOlder(ctx, alicePN.String(), 10, time.Second); err == nil || !strings.Contains(err.Error(), "no archived messages") {
		t.Errorf("empty chat: err = %v", err)
	}
	archive(t, c, incoming(alicePN, alicePN, "A5", 500, "newest"))

	// The phone answers while the request is being sent, so the wait ends at once.
	fake.onPeer = func(*waE2E.Message) {
		c.onHistorySync(historySync(waHistorySync.HistorySync_ON_DEMAND,
			&waHistorySync.Conversation{ID: proto.String(alicePN.String()),
				Messages: []*waHistorySync.HistorySyncMsg{webMsg("A1", false, 100, "", text("older"))}}))
	}
	res, err := c.FetchOlder(ctx, alicePN.String(), 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != FetchFetched || res.Fetched != 1 || res.Oldest.Unix() != 100 {
		t.Errorf("FetchOlder = %+v, want one fetched back to 100", res)
	}
	req := fake.peer[0].GetProtocolMessage().GetPeerDataOperationRequestMessage().GetHistorySyncOnDemandRequest()
	if req.GetOldestMsgID() != "A5" || req.GetOnDemandMsgCount() != 10 {
		t.Errorf("history request = %v, want 10 before A5", req)
	}
	if _, err := c.FetchOlder(ctx, alicePN.String(), 10, time.Minute); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("immediate second request: err = %v, want the rate limit", err)
	}

	// No answer in time.
	c, fake, _, _ = newTestClient(t)
	archive(t, c, incoming(alicePN, alicePN, "A5", 500, "newest"))
	res, err = c.FetchOlder(ctx, alicePN.String(), 10, 10*time.Millisecond)
	if err != nil || res.Status != FetchPending {
		t.Errorf("FetchOlder = %+v, err %v; want pending", res, err)
	}
	if len(fake.peer) != 1 {
		t.Errorf("pending fetch sent %d requests, want 1", len(fake.peer))
	}
	// The late answer is still archived.
	c.onHistorySync(historySync(waHistorySync.HistorySync_ON_DEMAND,
		&waHistorySync.Conversation{ID: proto.String(alicePN.String()),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("A1", false, 100, "", text("older"))}}))
	archived(t, c, alicePN, "A1")

	c, fake, _, _ = newTestClient(t)
	archive(t, c, incoming(alicePN, alicePN, "A5", 500, "newest"))
	fake.peerErr = errors.New("offline")
	if _, err := c.FetchOlder(ctx, alicePN.String(), 10, time.Second); err == nil || !strings.Contains(err.Error(), "ask phone") {
		t.Errorf("err = %v", err)
	}
	fake.loggedIn = false
	if _, err := c.FetchOlder(ctx, alicePN.String(), 10, time.Second); err == nil {
		t.Error("logged out: want an error")
	}
}

func TestStartStopStatus(t *testing.T) {
	ctx := context.Background()
	c, fake, _, _ := newTestClient(t)
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.connects != 1 || fake.qrCalls != 0 {
		t.Errorf("paired start: %d connects, %d QR channels; want 1 and 0", fake.connects, fake.qrCalls)
	}
	want := Status{Connected: true, LoggedIn: true, JID: ownJID.ToNonAD().String()}
	if got := c.Status(); got != want {
		t.Errorf("Status = %+v, want %+v", got, want)
	}
	c.Stop()
	if fake.disconnects != 1 {
		t.Errorf("%d disconnects, want 1", fake.disconnects)
	}

	// Unpaired: a QR channel first, then connect. The fake's channel is
	// already closed, so the pairing watcher ends at once.
	c, fake, _, _ = newTestClient(t)
	c.device.ID = nil
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	connects, qrCalls := fake.connects, fake.qrCalls
	fake.mu.Unlock()
	if connects != 1 || qrCalls != 1 {
		t.Errorf("unpaired start: %d connects, %d QR channels; want 1 and 1", connects, qrCalls)
	}
	if got := c.Status().JID; got != "" {
		t.Errorf("unpaired JID = %q", got)
	}

	c, fake, _, _ = newTestClient(t)
	c.device.ID = nil
	fake.connectErr = errors.New("offline")
	if err := c.Start(ctx); err == nil {
		t.Error("connect error: want an error")
	}
}

func TestWatchPairing(t *testing.T) {
	feed := func(items ...whatsmeow.QRChannelItem) <-chan whatsmeow.QRChannelItem {
		ch := make(chan whatsmeow.QRChannelItem, len(items))
		for _, it := range items {
			ch <- it
		}
		close(ch)
		return ch
	}
	code := whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "2@abc", Timeout: time.Minute}
	tests := []struct {
		name  string
		items []whatsmeow.QRChannelItem
		want  string
	}{
		{"code waiting", []whatsmeow.QRChannelItem{code}, "2@abc"},
		{"paired", []whatsmeow.QRChannelItem{code, whatsmeow.QRChannelSuccess}, ""},
		{"error", []whatsmeow.QRChannelItem{code, {Event: whatsmeow.QRChannelEventError, Error: errors.New("bad")}}, ""},
		{"timeout", []whatsmeow.QRChannelItem{code, whatsmeow.QRChannelTimeout}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _, _ := newTestClient(t)
			var out bytes.Buffer
			c.qrOut = &out
			c.watchPairing(feed(tt.items...))
			if got := c.QRCode(); got != tt.want {
				t.Errorf("QRCode = %q, want %q", got, tt.want)
			}
			if out.Len() == 0 {
				t.Error("no QR code printed")
			}
		})
	}
}

func TestOnMessageChangeAuthor(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	c.groupLookups.Store(testGroup.String(), struct{}{}) // no background name lookup
	edit := func(chat, sender types.JID, fromMe bool, target, to string) {
		e := liveMessage(chat, sender, "E-"+target, fromMe, "", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key:           &waCommon.MessageKey{ID: proto.String(target)},
			EditedMessage: text(to),
			TimestampMS:   proto.Int64(300_000),
		}})
		e.IsEdit = true
		c.onMessage(e)
	}
	revoke := func(chat, sender types.JID, fromMe bool, target string) {
		c.onMessage(liveMessage(chat, sender, "R-"+target, fromMe, "", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			Key:  &waCommon.MessageKey{ID: proto.String(target)},
		}}))
	}
	textOf := func(chat types.JID, id string) string {
		t.Helper()
		m, err := c.store.MessageByID(context.Background(), chat.String(), id)
		if errors.Is(err, store.ErrNotFound) {
			return "<deleted>"
		}
		if err != nil {
			t.Fatal(err)
		}
		return m.Text
	}

	c.onMessage(liveMessage(alicePN, aliceLID, "M1", false, "", text("hi")))
	c.onMessage(liveMessage(alicePN, ownJID, "O1", true, "", text("mine")))

	edit(alicePN, bobPN, false, "M1", "forged")
	revoke(alicePN, bobPN, false, "M1")
	edit(alicePN, alicePN, false, "O1", "forged")
	revoke(alicePN, alicePN, false, "O1")
	if got := textOf(alicePN, "M1"); got != "hi" {
		t.Errorf("M1 after others' changes = %q", got)
	}
	if got := textOf(alicePN, "O1"); got != "mine" {
		t.Errorf("O1 after others' changes = %q", got)
	}

	// The author may edit, also when the edit comes from her LID device.
	edit(alicePN, types.JID{User: aliceLID.User, Device: 3, Server: types.HiddenUserServer}, false, "M1", "hi there")
	if got := textOf(alicePN, "M1"); got != "hi there" {
		t.Errorf("M1 after Alice's edit = %q", got)
	}
	// The account's own devices: an own edit and a revoke apply.
	edit(alicePN, ownLID, true, "O1", "mine, edited")
	if got := textOf(alicePN, "O1"); got != "mine, edited" {
		t.Errorf("O1 after own edit = %q", got)
	}

	// In a group, a member may not delete another's message; an admin may.
	c.onMessage(liveMessage(testGroup, alicePN, "G1", false, "", text("group hi")))
	fake.groups[testGroup] = &types.GroupInfo{JID: testGroup, Participants: []types.GroupParticipant{
		{JID: alicePN}, {JID: bobLID, PhoneNumber: bobPN},
	}}
	revoke(testGroup, bobPN, false, "G1")
	if got := textOf(testGroup, "G1"); got != "group hi" {
		t.Errorf("G1 after a member's deletion = %q", got)
	}
	fake.groups[testGroup].Participants[1].IsAdmin = true
	edit(testGroup, bobLID, false, "G1", "forged")
	if got := textOf(testGroup, "G1"); got != "group hi" {
		t.Errorf("G1 after an admin's edit = %q", got)
	}
	revoke(testGroup, bobLID, false, "G1")
	if got := textOf(testGroup, "G1"); got != "<deleted>" {
		t.Errorf("G1 after an admin's deletion = %q", got)
	}
	revoke(alicePN, ownJID, true, "O1")
	if got := textOf(alicePN, "O1"); got != "<deleted>" {
		t.Errorf("O1 after own deletion = %q", got)
	}
}
