package store

import (
	"context"
	"time"
)

type ThemeConfigurationUpdate struct {
	PackageID string
	JSON      string
	At        time.Time
}

func (store *ThemeStore) UpdateConfiguration(ctx context.Context, update ThemeConfigurationUpdate) error {
	return store.mutate(ctx, themeWrite{"theme.configure", "theme_package", update.PackageID}, func(mutation themeTransaction) error {
		result, err := mutation.tx.ExecContext(ctx, `UPDATE theme_packages SET configuration_json=?,updated_at=? WHERE id=? AND state='installed'`, update.JSON, update.At, update.PackageID)
		return requireThemeRow(result, err, ErrThemePackageNotFound)
	})
}
