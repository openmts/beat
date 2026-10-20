package store

import "database/sql"

func acceleratorExactlyOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	if result == nil {
		return ErrAcceleratorUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrAcceleratorUnavailable
	}
	return nil
}
