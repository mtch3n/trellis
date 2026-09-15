package core

import (
	"context"
)

// Backup writes a consistent copy of the database with VACUUM INTO (§5).
// Copying the live file instead can capture a torn WAL.
func (c *Core) Backup(ctx context.Context, dest string) error {
	_, err := c.db.ExecContext(ctx, `VACUUM INTO ?`, dest)
	return err
}
