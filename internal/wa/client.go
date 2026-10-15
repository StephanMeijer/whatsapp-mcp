// SPDX-License-Identifier: MPL-2.0

// Package wa wraps the whatsmeow client: device storage, pairing, archiving
// incoming messages and sending.
package wa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// Client is a connected (or pairing) WhatsApp account.
type Client struct {
	cli   *whatsmeow.Client
	store *store.Store
	log   *slog.Logger
	// qrOut receives a terminal rendering of each pairing code.
	qrOut io.Writer

	mu sync.RWMutex
	qr string // current pairing code, empty when none is pending
}

// Status describes the connection.
type Status struct {
	Connected bool   `json:"connected"`
	LoggedIn  bool   `json:"logged_in"`
	JID       string `json:"jid,omitempty" jsonschema:"the account's own JID once paired"`
	Pairing   bool   `json:"pairing" jsonschema:"true while a QR code is waiting to be scanned (see GET /pair)"`
}

// Contact is an entry from the account's address book.
type Contact struct {
	JID      string `json:"jid"`
	Name     string `json:"name,omitempty"`
	PushName string `json:"push_name,omitempty"`
}

// New opens whatsmeow's device store on sqlDB (sharing the archive's
// connection pool) and creates a client for the first stored device, or a new
// one that will need pairing.
func New(ctx context.Context, sqlDB *sql.DB, dialect string, st *store.Store, log *slog.Logger, qrOut io.Writer) (*Client, error) {
	waLogger := newLogger(log.With("component", "whatsmeow"))
	container := sqlstore.NewWithDB(sqlDB, dialect, waLogger.Sub("store"))
	if err := container.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("upgrade whatsmeow store: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("load device: %w", err)
	}

	c := &Client{
		cli:   whatsmeow.NewClient(device, waLogger.Sub("client")),
		store: st,
		log:   log,
		qrOut: qrOut,
	}
	c.cli.AddEventHandler(c.handleEvent)
	return c, nil
}

// Start connects. An unpaired device starts the QR pairing flow: codes are
// printed to qrOut and available from QRCode until pairing completes.
func (c *Client) Start(ctx context.Context) error {
	if c.cli.Store.ID != nil {
		return c.cli.ConnectContext(ctx)
	}

	qrChan, err := c.cli.GetQRChannel(context.WithoutCancel(ctx))
	if err != nil {
		return fmt.Errorf("start pairing: %w", err)
	}
	if err := c.cli.ConnectContext(ctx); err != nil {
		return err
	}
	go c.watchPairing(qrChan)
	return nil
}

func (c *Client) watchPairing(qrChan <-chan whatsmeow.QRChannelItem) {
	for item := range qrChan {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			c.setQR(item.Code)
			c.log.Info("scan the QR code with WhatsApp > Linked devices (also served at GET /pair)",
				"expires_in", item.Timeout.Round(time.Second))
			qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, c.qrOut)
		case whatsmeow.QRChannelSuccess.Event:
			c.setQR("")
			c.log.Info("paired", "jid", c.cli.Store.ID.String())
		case whatsmeow.QRChannelEventError:
			c.setQR("")
			c.log.Error("pairing failed", "err", item.Error)
		default:
			c.setQR("")
			c.log.Warn("pairing ended; restart to get a new QR code", "event", item.Event)
		}
	}
}

func (c *Client) setQR(code string) {
	c.mu.Lock()
	c.qr = code
	c.mu.Unlock()
}

// QRCode returns the pairing code waiting to be scanned, or "" if none.
func (c *Client) QRCode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.qr
}

// Stop disconnects.
func (c *Client) Stop() { c.cli.Disconnect() }

// Status reports the connection state.
func (c *Client) Status() Status {
	s := Status{
		Connected: c.cli.IsConnected(),
		LoggedIn:  c.cli.IsLoggedIn(),
		Pairing:   c.QRCode() != "",
	}
	if id := c.cli.Store.ID; id != nil {
		s.JID = id.ToNonAD().String()
	}
	return s
}

// Contacts returns address-book entries whose name, push name or JID contains
// query (case-insensitive), sorted by name.
func (c *Client) Contacts(ctx context.Context, query string) ([]Contact, error) {
	all, err := c.cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	out := make([]Contact, 0, len(all))
	for jid, info := range all {
		ct := Contact{JID: jid.String(), Name: contactName(info), PushName: info.PushName}
		hay := strings.ToLower(ct.JID + "\x00" + ct.Name + "\x00" + ct.PushName)
		if strings.Contains(hay, q) {
			out = append(out, ct)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].JID < out[j].JID
	})
	return out, nil
}

// SendText sends a text message and archives it. recipient is a JID or a
// phone number in international format (digits, optional leading +).
func (c *Client) SendText(ctx context.Context, recipient, text string) (store.Message, error) {
	if !c.cli.IsLoggedIn() {
		return store.Message{}, errors.New("not logged in to WhatsApp")
	}
	to, err := ParseRecipient(recipient)
	if err != nil {
		return store.Message{}, err
	}
	resp, err := c.cli.SendMessage(ctx, to, &waE2E.Message{Conversation: proto.String(text)})
	if err != nil {
		return store.Message{}, fmt.Errorf("send: %w", err)
	}

	m := store.Message{
		ChatJID:   to.String(),
		MessageID: resp.ID,
		SenderJID: c.cli.Store.ID.ToNonAD().String(),
		Timestamp: resp.Timestamp.Unix(),
		FromMe:    true,
		Text:      text,
	}
	if err := c.store.Save(ctx, nil, []store.Message{m}); err != nil {
		// The message went out; failing to archive it shouldn't fail the send.
		c.log.Error("archive sent message", "err", err, "id", resp.ID)
	}
	return m, nil
}

// ParseRecipient accepts a full JID ("31612345678@s.whatsapp.net",
// "123-456@g.us") or a phone number ("+31 6 1234 5678").
func ParseRecipient(s string) (types.JID, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "@") {
		jid, err := types.ParseJID(s)
		if err != nil {
			return types.JID{}, fmt.Errorf("invalid JID %q: %w", s, err)
		}
		return jid, nil
	}
	digits := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9':
			return r
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')':
			return -1
		default:
			return 'x'
		}
	}, s)
	if digits == "" || strings.Contains(digits, "x") {
		return types.JID{}, fmt.Errorf("invalid recipient %q: want a JID or an international phone number", s)
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

// contactName is the name the user saved for a contact, or the business name.
func contactName(info types.ContactInfo) string {
	return firstNonEmpty(info.FullName, info.FirstName, info.BusinessName)
}

// syncChatNames names direct chats after the matching contact: the saved
// name if there is one, otherwise the contact's push name. History sync only
// names groups, so without this most direct chats would show a bare JID.
func (c *Client) syncChatNames(ctx context.Context) {
	contacts, err := c.cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		c.log.Error("sync chat names: load contacts", "err", err)
		return
	}
	names := make(map[string]string, len(contacts))
	for jid, info := range contacts {
		names[jid.String()] = firstNonEmpty(contactName(info), info.PushName)
	}
	changed, err := c.store.RenameChats(ctx, names)
	if err != nil {
		c.log.Error("sync chat names", "err", err)
		return
	}
	if changed > 0 {
		c.log.Info("named chats from contacts", "chats", changed)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
