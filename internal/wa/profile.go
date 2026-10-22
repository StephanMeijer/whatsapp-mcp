// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// ContactInfo is what WhatsApp shows about a person on their profile.
type ContactInfo struct {
	JID          string `json:"jid"`
	Name         string `json:"name,omitempty" jsonschema:"the saved contact name, otherwise their WhatsApp push name"`
	About        string `json:"about,omitempty" jsonschema:"their about text; absent if empty or hidden from you"`
	BusinessName string `json:"business_name,omitempty" jsonschema:"the verified business name, if it is a business account"`
	HasPicture   bool   `json:"has_picture"`
}

var (
	ErrPictureHidden = errors.New("this profile picture is hidden from you")
	ErrNoPicture     = errors.New("no profile picture is set")
)

// ContactInfo asks WhatsApp for a person's profile info.
func (c *Client) ContactInfo(ctx context.Context, jid types.JID) (ContactInfo, error) {
	if !c.cli.IsLoggedIn() {
		return ContactInfo{}, errors.New("not logged in to WhatsApp")
	}
	jid = c.Canonical(ctx, jid)
	infos, err := c.cli.GetUserInfo(ctx, []types.JID{jid})
	if err != nil {
		return ContactInfo{}, fmt.Errorf("contact info: %w", err)
	}
	info, ok := infos[jid]
	if !ok {
		return ContactInfo{}, fmt.Errorf("WhatsApp has no info for %s", jid)
	}
	names, err := c.Names(ctx, []types.JID{jid})
	if err != nil {
		c.log.Warn("contact info: look up name", "err", err, "jid", jid.String())
	}
	return contactInfo(jid, info, names[jid]), nil
}

func contactInfo(jid types.JID, info types.UserInfo, name string) ContactInfo {
	out := ContactInfo{JID: jid.String(), Name: name, About: info.Status, HasPicture: info.PictureID != ""}
	if info.VerifiedName != nil {
		out.BusinessName = info.VerifiedName.Details.GetVerifiedName()
	}
	return out
}

// ProfilePicture downloads a contact's or group's profile picture, the
// small thumbnail when preview is set.
// shortcut: community photos may come back as hidden (401); add an
// IsCommunity retry when someone needs them.
func (c *Client) ProfilePicture(ctx context.Context, jid types.JID, preview bool) (Media, error) {
	if !c.cli.IsLoggedIn() {
		return Media{}, errors.New("not logged in to WhatsApp")
	}
	info, err := c.cli.GetProfilePictureInfo(ctx, jid.ToNonAD(), &whatsmeow.GetProfilePictureParams{Preview: preview})
	switch {
	case errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
		return Media{}, ErrPictureHidden
	case errors.Is(err, whatsmeow.ErrProfilePictureNotSet), err == nil && info == nil:
		return Media{}, ErrNoPicture
	case err != nil:
		return Media{}, fmt.Errorf("profile picture: %w", err)
	}
	return fetchPicture(ctx, info.URL)
}

// fetchPicture downloads a profile picture from the URL WhatsApp gave.
func fetchPicture(ctx context.Context, url string) (Media, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Media{}, fmt.Errorf("profile picture: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Media{}, fmt.Errorf("download profile picture: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Media{}, fmt.Errorf("download profile picture: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxMediaBytes+1))
	if err != nil {
		return Media{}, fmt.Errorf("download profile picture: %w", err)
	}
	if len(data) > MaxMediaBytes {
		return Media{}, fmt.Errorf("profile picture is over the %d MB limit", MaxMediaBytes>>20)
	}
	return Media{Kind: "image", MIMEType: http.DetectContentType(data), Data: data}, nil
}
