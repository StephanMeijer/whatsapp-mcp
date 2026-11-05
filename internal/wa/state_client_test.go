// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestOnReceipt(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	ctx := context.Background()
	own := func(id string) store.Message {
		return store.Message{ChatJID: alicePN.String(), MessageID: id, SenderJID: ownJID.ToNonAD().String(), Timestamp: 100, FromMe: true, Text: id}
	}
	archive(t, c, own("S1"), own("S2"), own("S3"), own("S4"), own("S5"))
	aliceDevice := types.JID{User: aliceLID.User, Device: 3, Server: types.HiddenUserServer}
	receipt := func(id string, typ types.ReceiptType, messageSender types.JID) *events.Receipt {
		return &events.Receipt{
			MessageSource: types.MessageSource{Chat: aliceLID, Sender: aliceDevice},
			MessageIDs:    []types.MessageID{id},
			Timestamp:     time.Unix(200, 0),
			Type:          typ,
			MessageSender: messageSender,
		}
	}
	c.onReceipt(receipt("S1", types.ReceiptTypeRead, types.EmptyJID))
	c.onReceipt(receipt("S2", types.ReceiptTypeRead, bobPN))   // the account reading Bob's message elsewhere
	c.onReceipt(receipt("S3", types.ReceiptTypeRead, ownLID))  // the account, by LID
	c.onReceipt(receipt("S4", types.ReceiptTypeRead, ownJID))  // the account, by number
	c.onReceipt(receipt("S5", types.ReceiptTypeRetry, ownJID)) // not a receipt that counts

	want := map[string]string{"S1": store.StatusRead, "S2": store.StatusSent, "S3": store.StatusRead, "S4": store.StatusRead, "S5": store.StatusSent}
	for id, status := range want {
		if got := archived(t, c, alicePN, id).Status; got != status {
			t.Errorf("%s status = %q, want %q", id, got, status)
		}
	}
	rs, err := c.store.Receipts(ctx, alicePN.String(), "S1")
	if err != nil || len(rs) != 1 || rs[0].ParticipantJID != alicePN.String() || rs[0].ReadAt != 200 {
		t.Errorf("receipts for S1 = %+v, err %v; want alice read at 200", rs, err)
	}
}

func TestOnChatState(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	ctx := context.Background()
	if err := c.store.Save(ctx, []store.Chat{{JID: alicePN.String()}}, nil); err != nil {
		t.Fatal(err)
	}
	// By LID, as the phone may send it; applied to the number.
	c.onChatState(&events.Archive{JID: aliceLID, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}})
	c.onChatState(&events.Pin{JID: alicePN}) // no action: ignored
	chats, err := c.store.ChatsByJID(ctx, []string{alicePN.String()})
	if err != nil {
		t.Fatal(err)
	}
	if ch := chats[alicePN.String()]; !ch.Archived || ch.Pinned {
		t.Errorf("chat = %+v, want archived and not pinned", ch)
	}
}

func TestSetChatState(t *testing.T) {
	ctx := context.Background()
	indexes := func(f *fakeWA) []string {
		var out []string
		for _, p := range f.patches {
			out = append(out, p.Mutations[0].Index[0])
		}
		return out
	}
	tests := []struct {
		name  string
		flags store.ChatFlags
		want  []string
	}{
		{"archiving unpins, so no pin is sent", store.ChatFlags{Archived: new(true), Pinned: new(true)}, []string{"archive"}},
		{"pin", store.ChatFlags{Pinned: new(true)}, []string{"pin_v1"}},
		{"unarchive and pin", store.ChatFlags{Archived: new(false), Pinned: new(true)}, []string{"archive", "pin_v1"}},
		{"mute until", store.ChatFlags{MutedUntil: new(int64(1_700_000_000))}, []string{"mute"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, fake, _, _ := newTestClient(t)
			if err := c.SetChatState(ctx, alicePN, tt.flags); err != nil {
				t.Fatal(err)
			}
			if got := indexes(fake); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("patches = %v, want %v", got, tt.want)
			}
		})
	}

	c, fake, _, _ := newTestClient(t)
	if err := c.SetChatState(ctx, alicePN, store.ChatFlags{MutedUntil: new(int64(1_700_000_000))}); err != nil {
		t.Fatal(err)
	}
	mute := fake.patches[0].Mutations[0].Value.GetMuteAction()
	if !mute.GetMuted() || mute.GetMuteEndTimestamp() != 1_700_000_000_000 {
		t.Errorf("mute action = %v, want muted until 1700000000000 ms", mute)
	}
	if err := c.SetChatState(ctx, alicePN, store.ChatFlags{MutedUntil: new(int64(0))}); err != nil {
		t.Fatal(err)
	}
	if fake.patches[1].Mutations[0].Value.GetMuteAction().GetMuted() {
		t.Error("MutedUntil 0 should unmute")
	}

	c, fake, _, _ = newTestClient(t)
	fake.appStateErrAt = 1
	err := c.SetChatState(ctx, alicePN, store.ChatFlags{Archived: new(false), Pinned: new(true)})
	if err == nil || !strings.Contains(err.Error(), "archive chat:") || len(fake.patches) != 1 {
		t.Errorf("err = %v after %d patches; want the archive error and no pin", err, len(fake.patches))
	}

	fake.loggedIn = false
	if err := c.SetChatState(ctx, alicePN, store.ChatFlags{Pinned: new(true)}); err == nil || len(fake.patches) != 1 {
		t.Errorf("logged out: err %v, %d patches", err, len(fake.patches))
	}
}

func TestClientGroupInfo(t *testing.T) {
	c, fake, _, contacts := newTestClient(t)
	addressBook(contacts)
	ctx := context.Background()
	fake.groups[testGroup] = &types.GroupInfo{
		JID:       testGroup,
		GroupName: types.GroupName{Name: "Family"},
		Participants: []types.GroupParticipant{
			{JID: aliceLID, PhoneNumber: alicePN, IsAdmin: true},
			{JID: bobLID, IsSuperAdmin: true},
			{JID: ownJID.ToNonAD()},
		},
	}
	g, err := c.GroupInfo(ctx, testGroup)
	if err != nil {
		t.Fatal(err)
	}
	want := []GroupMember{
		{JID: alicePN.String(), Name: "Alice Saved", Role: "admin"},
		{JID: bobPN.String(), Name: "Bob", Role: "super admin"},
		{JID: ownJID.ToNonAD().String(), Name: "Me", Role: "member"},
	}
	if g.Name != "Family" || !reflect.DeepEqual(g.Members, want) {
		t.Errorf("GroupInfo = %+v\nwant members %+v", g, want)
	}
	if !reflect.DeepEqual(fake.groupInfoCalls, []types.JID{testGroup}) {
		t.Errorf("looked up %v", fake.groupInfoCalls)
	}

	fake.groupErr = errors.New("offline")
	if _, err := c.GroupInfo(ctx, testGroup); err == nil || !strings.Contains(err.Error(), "group info:") {
		t.Errorf("err = %v", err)
	}
	fake.loggedIn = false
	if _, err := c.GroupInfo(ctx, testGroup); err == nil {
		t.Error("logged out: want an error")
	}
}

func TestClientContactInfo(t *testing.T) {
	c, fake, _, contacts := newTestClient(t)
	addressBook(contacts)
	ctx := context.Background()
	fake.users = map[types.JID]types.UserInfo{alicePN: {Status: "Busy", PictureID: "123"}}

	// Asked by LID, looked up and answered by number.
	got, err := c.ContactInfo(ctx, aliceLID)
	if err != nil {
		t.Fatal(err)
	}
	want := ContactInfo{JID: alicePN.String(), Name: "Alice Saved", About: "Busy", HasPicture: true}
	if got != want {
		t.Errorf("ContactInfo = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(fake.userInfoCalls, [][]types.JID{{alicePN}}) {
		t.Errorf("looked up %v", fake.userInfoCalls)
	}

	if _, err := c.ContactInfo(ctx, bobPN); err == nil || !strings.Contains(err.Error(), "has no info") {
		t.Errorf("unknown user: err = %v", err)
	}
	fake.userErr = errors.New("offline")
	if _, err := c.ContactInfo(ctx, alicePN); err == nil || !strings.Contains(err.Error(), "contact info:") {
		t.Errorf("err = %v", err)
	}
	fake.loggedIn = false
	if _, err := c.ContactInfo(ctx, alicePN); err == nil {
		t.Error("logged out: want an error")
	}
}

func TestProfilePicture(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name    string
		info    *types.ProfilePictureInfo
		err     error
		wantErr error
		wantMsg string
	}{
		{"hidden", nil, whatsmeow.ErrProfilePictureUnauthorized, ErrPictureHidden, ""},
		{"not set", nil, whatsmeow.ErrProfilePictureNotSet, ErrNoPicture, ""},
		{"no info", nil, nil, ErrNoPicture, ""},
		{"other error", nil, errors.New("offline"), nil, "profile picture:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, fake, _, _ := newTestClient(t)
			fake.picture, fake.pictureErr = tt.info, tt.err
			_, err := c.ProfilePicture(ctx, alicePN, false)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantMsg != "" && (err == nil || !strings.Contains(err.Error(), tt.wantMsg)) {
				t.Errorf("err = %v, want one containing %q", err, tt.wantMsg)
			}
		})
	}

	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte("x"), 100)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(jpeg) }))
	defer srv.Close()
	c, fake, _, _ := newTestClient(t)
	fake.picture = &types.ProfilePictureInfo{URL: srv.URL + "/pic.jpg"}
	got, err := c.ProfilePicture(ctx, alicePN, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "image" || !bytes.Equal(got.Data, jpeg) {
		t.Errorf("picture = kind %q, %d bytes", got.Kind, len(got.Data))
	}
	if len(fake.pictureParams) != 1 || !fake.pictureParams[0].Preview {
		t.Errorf("params = %+v, want a preview", fake.pictureParams)
	}
	fake.loggedIn = false
	if _, err := c.ProfilePicture(ctx, alicePN, false); err == nil {
		t.Error("logged out: want an error")
	}
}
