// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

// People and chats the tests use. Alice's and Bob's LIDs map to their phone
// numbers; the stranger's LID maps to nothing.
var (
	ownJID    = types.NewADJID("31600000000", 0, 7)
	ownLID    = types.JID{User: "900", Device: 7, Server: types.HiddenUserServer}
	alicePN   = types.NewJID("31600000001", types.DefaultUserServer)
	aliceLID  = types.NewJID("111", types.HiddenUserServer)
	bobPN     = types.NewJID("31600000002", types.DefaultUserServer)
	bobLID    = types.NewJID("222", types.HiddenUserServer)
	stranger  = types.NewJID("999", types.HiddenUserServer)
	testGroup = types.NewJID("120363000000000001", types.GroupServer)
)

// sendTime is when the fake says every message it sends was sent.
var sendTime = time.Unix(1_700_000_000, 0)

// newTestClient returns a Client on a fresh archive, talking to a fake
// WhatsApp. The device is paired as ownJID, and the LID store knows Alice's
// and Bob's numbers.
func newTestClient(t *testing.T) (*Client, *fakeWA, *memLIDs, *memContacts) {
	t.Helper()
	id := ownJID
	lids := &memLIDs{pnByLID: map[string]string{aliceLID.User: alicePN.User, bobLID.User: bobPN.User}}
	contacts := &memContacts{all: map[types.JID]types.ContactInfo{}}
	dev := &wastore.Device{ID: &id, LID: ownLID, PushName: "Me", LIDs: lids, Contacts: contacts}
	fake := &fakeWA{
		real:      whatsmeow.NewClient(dev, nil),
		loggedIn:  true,
		connected: true,
		groups:    map[types.JID]*types.GroupInfo{},
		decrypted: map[string]*waE2E.Message{},
	}
	c := newClient(fake, dev, storetest.New(t), slog.New(slog.DiscardHandler), io.Discard)
	return c, fake, lids, contacts
}

type sentMsg struct {
	to  types.JID
	msg *waE2E.Message
}

type readCall struct {
	ids    []types.MessageID
	latest time.Time
	chat   types.JID
	sender types.JID
}

type rejectCall struct {
	from types.JID
	id   string
}

// fakeWA stands in for *whatsmeow.Client. Calls that would reach WhatsApp
// are recorded and answered from its fields. Building and parsing messages
// needs no network, so those calls go to real, a whatsmeow client that never
// connects, on the same device as the Client under test.
//
// It does not embed *whatsmeow.Client: a method it lacks must fail to
// compile, not quietly reach the network.
type fakeWA struct {
	real *whatsmeow.Client

	mu          sync.Mutex
	loggedIn    bool
	connected   bool
	connects    int
	disconnects int
	qr          chan whatsmeow.QRChannelItem
	qrCalls     int

	sent     []sentMsg
	peer     []*waE2E.Message
	patches  []appstate.PatchInfo
	reads    []readCall
	rejected []rejectCall

	groupInfoCalls []types.JID
	userInfoCalls  [][]types.JID
	pictureParams  []*whatsmeow.GetProfilePictureParams
	uploads        []whatsmeow.MediaType
	lookups        [][]string

	nextID        int // SendMessage answers with IDs OUT1, OUT2, ...
	sendErr       error
	peerErr       error
	readErr       error
	appStateErrAt int // the n-th SendAppState call fails (1-based); 0 never
	rejectErr     error
	connectErr    error

	groups      map[types.JID]*types.GroupInfo
	groupErr    error
	joined      []*types.GroupInfo
	joinedErr   error
	users       map[types.JID]types.UserInfo
	userErr     error
	picture     *types.ProfilePictureInfo
	pictureErr  error
	onWhatsApp  []types.IsOnWhatsAppResponse
	lookupErr   error
	upload      whatsmeow.UploadResponse
	uploadErr   error
	download    []byte
	downloadErr error
	decrypted   map[string]*waE2E.Message // by target message ID; missing is an error

	// onPeer, when set, runs inside SendPeerMessage, for example to deliver
	// the phone's answer to a history request.
	onPeer func(*waE2E.Message)
}

var _ whatsmeowClient = (*fakeWA)(nil)

func (f *fakeWA) IsConnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected
}

func (f *fakeWA) IsLoggedIn() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loggedIn
}

func (f *fakeWA) ConnectContext(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	return f.connectErr
}

func (f *fakeWA) Disconnect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnects++
}

func (f *fakeWA) GetQRChannel(context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.qrCalls++
	if f.qr == nil {
		f.qr = make(chan whatsmeow.QRChannelItem)
		close(f.qr)
	}
	return f.qr, nil
}

func (f *fakeWA) SendMessage(_ context.Context, to types.JID, msg *waE2E.Message, _ ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return whatsmeow.SendResponse{}, f.sendErr
	}
	f.sent = append(f.sent, sentMsg{to, msg})
	f.nextID++
	return whatsmeow.SendResponse{ID: fmt.Sprintf("OUT%d", f.nextID), Timestamp: sendTime}, nil
}

func (f *fakeWA) SendPeerMessage(_ context.Context, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
	f.mu.Lock()
	if f.peerErr != nil {
		defer f.mu.Unlock()
		return whatsmeow.SendResponse{}, f.peerErr
	}
	f.peer = append(f.peer, msg)
	f.nextID++
	resp := whatsmeow.SendResponse{ID: fmt.Sprintf("PEER%d", f.nextID), Timestamp: sendTime}
	hook := f.onPeer
	f.mu.Unlock()
	if hook != nil {
		hook(msg)
	}
	return resp, nil
}

func (f *fakeWA) SendAppState(_ context.Context, patch appstate.PatchInfo) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patches = append(f.patches, patch)
	if f.appStateErrAt == len(f.patches) {
		return fmt.Errorf("app state patch %d refused", len(f.patches))
	}
	return nil
}

func (f *fakeWA) MarkRead(_ context.Context, ids []types.MessageID, ts time.Time, chat, sender types.JID, _ ...types.ReceiptType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return f.readErr
	}
	f.reads = append(f.reads, readCall{ids: ids, latest: ts, chat: chat, sender: sender})
	return nil
}

func (f *fakeWA) RejectCall(_ context.Context, from types.JID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectErr != nil {
		return f.rejectErr
	}
	f.rejected = append(f.rejected, rejectCall{from, id})
	return nil
}

func (f *fakeWA) Upload(_ context.Context, _ []byte, kind whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, kind)
	return f.upload, f.uploadErr
}

func (f *fakeWA) Download(context.Context, whatsmeow.DownloadableMessage) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.download, f.downloadErr
}

func (f *fakeWA) GetGroupInfo(_ context.Context, jid types.JID) (*types.GroupInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groupInfoCalls = append(f.groupInfoCalls, jid)
	if f.groupErr != nil {
		return nil, f.groupErr
	}
	info, ok := f.groups[jid]
	if !ok {
		return nil, fmt.Errorf("no such group %s", jid)
	}
	return info, nil
}

func (f *fakeWA) GetJoinedGroups(context.Context) ([]*types.GroupInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.joined, f.joinedErr
}

func (f *fakeWA) GetUserInfo(_ context.Context, jids []types.JID) (map[types.JID]types.UserInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.userInfoCalls = append(f.userInfoCalls, jids)
	return f.users, f.userErr
}

func (f *fakeWA) GetProfilePictureInfo(_ context.Context, _ types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pictureParams = append(f.pictureParams, params)
	return f.picture, f.pictureErr
}

func (f *fakeWA) IsOnWhatsApp(_ context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups = append(f.lookups, phones)
	return f.onWhatsApp, f.lookupErr
}

func (f *fakeWA) DecryptSecretEncryptedMessage(_ context.Context, evt *events.Message) (*waE2E.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := evt.Message.GetSecretEncryptedMessage().GetTargetMessageKey().GetID()
	if m, ok := f.decrypted[id]; ok {
		return m, nil
	}
	return nil, whatsmeow.ErrOriginalMessageSecretNotFound
}

func (f *fakeWA) BuildMessageKey(chat, sender types.JID, id types.MessageID) *waCommon.MessageKey {
	return f.real.BuildMessageKey(chat, sender, id)
}

func (f *fakeWA) BuildReaction(chat, sender types.JID, id types.MessageID, reaction string) *waE2E.Message {
	return f.real.BuildReaction(chat, sender, id, reaction)
}

func (f *fakeWA) BuildRevoke(chat, sender types.JID, id types.MessageID) *waE2E.Message {
	return f.real.BuildRevoke(chat, sender, id)
}

func (f *fakeWA) BuildEdit(chat types.JID, id types.MessageID, newContent *waE2E.Message) *waE2E.Message {
	return f.real.BuildEdit(chat, id, newContent)
}

func (f *fakeWA) BuildHistorySyncRequest(last *types.MessageInfo, count int) *waE2E.Message {
	return f.real.BuildHistorySyncRequest(last, count)
}

func (f *fakeWA) ParseWebMessage(chat types.JID, msg *waWeb.WebMessageInfo) (*events.Message, error) {
	return f.real.ParseWebMessage(chat, msg)
}

// memLIDs is an in-memory LID store with the lookup semantics of whatsmeow's
// sqlstore.CachedLIDMap: a JID on the wrong server is an error, an unknown
// one gives an empty JID and no error, and a result keeps the device part of
// the JID looked up. Methods the package does not use panic.
type memLIDs struct {
	wastore.LIDStore
	mu      sync.Mutex
	pnByLID map[string]string // LID user to phone-number user
	err     error
}

func (m *memLIDs) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return types.JID{}, m.err
	}
	if lid.Server != types.HiddenUserServer {
		return types.JID{}, fmt.Errorf("invalid GetPNForLID call with non-LID JID %s", lid)
	}
	pn, ok := m.pnByLID[lid.User]
	if !ok {
		return types.JID{}, nil
	}
	return types.JID{User: pn, Device: lid.Device, Server: types.DefaultUserServer}, nil
}

func (m *memLIDs) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return types.JID{}, m.err
	}
	if pn.Server != types.DefaultUserServer {
		return types.JID{}, fmt.Errorf("invalid GetLIDForPN call with non-PN JID %s", pn)
	}
	for lid, p := range m.pnByLID {
		if p == pn.User {
			return types.JID{User: lid, Device: pn.Device, Server: types.HiddenUserServer}, nil
		}
	}
	return types.JID{}, nil
}

// memContacts is an in-memory contact store. Methods the package does not
// use panic.
type memContacts struct {
	wastore.ContactStore
	mu  sync.Mutex
	all map[types.JID]types.ContactInfo
	err error
}

func (m *memContacts) GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	out := make(map[types.JID]types.ContactInfo, len(m.all))
	for k, v := range m.all {
		out[k] = v
	}
	return out, nil
}

// New must give the Client the very device the whatsmeow client uses, since
// whatsmeow updates it in place on pairing.
func TestNewWiresDeviceStore(t *testing.T) {
	d := storetest.NewDB(t)
	st := storetest.New(t)
	c, err := New(context.Background(), d.SQL, d.Dialect, st, slog.New(slog.DiscardHandler), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	real, ok := c.cli.(*whatsmeow.Client)
	if !ok {
		t.Fatalf("cli is %T, want *whatsmeow.Client", c.cli)
	}
	if real.Store != c.device {
		t.Error("Client.device is not the whatsmeow client's Store")
	}
	if got := c.Status(); got != (Status{}) {
		t.Errorf("unpaired Status = %+v, want all zero", got)
	}
}

// archive saves messages straight into the test client's archive.
func archive(t *testing.T, c *Client, msgs ...store.Message) {
	t.Helper()
	if err := c.store.Save(context.Background(), nil, msgs); err != nil {
		t.Fatal(err)
	}
}

// archived returns one message from the test client's archive.
func archived(t *testing.T, c *Client, chat types.JID, id string) store.Message {
	t.Helper()
	m, err := c.store.MessageByID(context.Background(), chat.String(), id)
	if err != nil {
		t.Fatalf("message %s in %s: %v", id, chat, err)
	}
	return m
}

// incoming is a text message from sender in chat.
func incoming(chat, sender types.JID, id string, ts int64, text string) store.Message {
	return store.Message{ChatJID: chat.String(), MessageID: id, SenderJID: sender.String(), Timestamp: ts, Text: text}
}

// chatNames returns the archived names of the given chats; absent chats are missing.
func chatNames(t *testing.T, c *Client, jids ...types.JID) map[string]string {
	t.Helper()
	keys := make([]string, len(jids))
	for i, j := range jids {
		keys[i] = j.String()
	}
	chats, err := c.store.ChatsByJID(context.Background(), keys)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, ch := range chats {
		out[k] = ch.Name
	}
	return out
}
