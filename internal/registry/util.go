package registry

import (
	"database/sql"
	"strings"
)

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

func isDuplicate(err error) bool { return err != nil && strings.Contains(err.Error(), "Duplicate entry") }
