package emailservice

import (
	"context"
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"

	"nexflow/internal/models"
)

// ListMailboxes connects with the supplied credentials and returns every
// mailbox/folder name visible to that account. Used by the settings UI to
// populate the folder dropdown — we don't want admins typing folder names
// by hand and getting silent select-failed polls.
func ListMailboxes(_ context.Context, a *models.IMAPAccount) ([]string, error) {
	addr := fmt.Sprintf("%s:%d", a.Host, a.Port)
	c, err := imapclient.DialTLS(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer c.Close()

	if err := c.Authenticate(sasl.NewPlainClient("", a.Username, a.Password)); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	// 8s deadline to keep the API responsive — folder lists are tiny.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	listCmd := c.List("", "*", nil)
	defer listCmd.Close()

	var folders []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			data := listCmd.Next()
			if data == nil {
				return
			}
			if data.Mailbox != "" && data.Attrs != nil {
				if hasAttr(data.Attrs, imap.MailboxAttrNoSelect) {
					continue
				}
			}
			if data.Mailbox != "" {
				folders = append(folders, data.Mailbox)
			}
		}
	}()

	select {
	case <-done:
		return folders, nil
	case <-ctx.Done():
		return folders, fmt.Errorf("list folders timeout")
	}
}

func hasAttr(attrs []imap.MailboxAttr, want imap.MailboxAttr) bool {
	for _, a := range attrs {
		if a == want {
			return true
		}
	}
	return false
}
