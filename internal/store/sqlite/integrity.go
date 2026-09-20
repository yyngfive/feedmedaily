package sqlite

import (
	"context"
	"fmt"
)

func (s *Store) QuickCheck(ctx context.Context) error {
	var result string
	if err := s.db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("database integrity check: %s", result)
	}
	return nil
}
