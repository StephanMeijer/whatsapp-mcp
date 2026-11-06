// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"time"
)

// ExpireDisappearing deletes archived messages from disappearing chats once
// their timer has run out: now, and then every interval until Stop. Messages
// archived without a timer are never touched.
func (c *Client) ExpireDisappearing(interval time.Duration) {
	c.bg.Go(func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			c.expireDue()
			select {
			case <-c.bgCtx.Done():
				return
			case <-tick.C:
			}
		}
	})
}

func (c *Client) expireDue() {
	ctx, cancel := context.WithTimeout(c.bgCtx, saveTimeout)
	defer cancel()
	n, err := c.store.ExpireMessages(ctx, time.Now().Unix())
	if err != nil {
		c.log.Error("expire disappearing messages", "err", err)
		return
	}
	if n > 0 {
		c.log.Info("expired disappearing messages", "messages", n)
	}
}
