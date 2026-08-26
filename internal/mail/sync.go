package mail

import (
	"context"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/store"
)

// Sync fetches inbox messages from the platform mail source (Apple Mail on
// macOS, Thunderbird on Linux) and replaces this source's rows in the local
// store with them, returning the synced messages and the distinct account
// names now in the store. Shared by the CLI sync command, MCP sync tool,
// and TUI — all three previously duplicated this fetch+store+DeleteBySource+
// UpsertMessage loop verbatim.
func Sync(count int) (msgs []models.Message, accounts []string, err error) {
	msgs, err = FetchInbox(count, false)
	if err != nil {
		return nil, nil, err
	}
	s, err := store.New(config.DBPath(), config.Shared())
	if err != nil {
		return nil, nil, err
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.DeleteBySource(ctx, sourceName)
	for i := range msgs {
		_ = s.UpsertMessage(ctx, &msgs[i])
	}
	accounts, _ = s.ListAccounts(ctx)
	return msgs, accounts, nil
}
