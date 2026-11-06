// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// whatsmeowClient is the part of *whatsmeow.Client this package uses, so
// tests can substitute a fake. The device store (own JID, LID mappings,
// contacts) is reached through Client.device, not through this interface.
type whatsmeowClient interface {
	IsConnected() bool
	IsLoggedIn() bool
	ConnectContext(ctx context.Context) error
	Disconnect()
	GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error)

	SendMessage(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error)
	SendPeerMessage(ctx context.Context, message *waE2E.Message) (whatsmeow.SendResponse, error)
	SendAppState(ctx context.Context, patch appstate.PatchInfo) error
	MarkRead(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error
	RejectCall(ctx context.Context, callFrom types.JID, callID string) error
	Upload(ctx context.Context, plaintext []byte, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
	DownloadToFile(ctx context.Context, msg whatsmeow.DownloadableMessage, file whatsmeow.File) error

	GetGroupInfo(ctx context.Context, jid types.JID) (*types.GroupInfo, error)
	GetJoinedGroups(ctx context.Context) ([]*types.GroupInfo, error)
	GetUserInfo(ctx context.Context, jids []types.JID) (map[types.JID]types.UserInfo, error)
	GetProfilePictureInfo(ctx context.Context, jid types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error)
	IsOnWhatsApp(ctx context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error)
	DecryptSecretEncryptedMessage(ctx context.Context, evt *events.Message) (*waE2E.Message, error)

	// These build or decode messages without touching the network.
	BuildMessageKey(chat, sender types.JID, id types.MessageID) *waCommon.MessageKey
	BuildReaction(chat, sender types.JID, id types.MessageID, reaction string) *waE2E.Message
	BuildRevoke(chat, sender types.JID, id types.MessageID) *waE2E.Message
	BuildEdit(chat types.JID, id types.MessageID, newContent *waE2E.Message) *waE2E.Message
	BuildHistorySyncRequest(lastKnownMessageInfo *types.MessageInfo, count int) *waE2E.Message
	ParseWebMessage(chatJID types.JID, webMsg *waWeb.WebMessageInfo) (*events.Message, error)
}

var _ whatsmeowClient = (*whatsmeow.Client)(nil)
