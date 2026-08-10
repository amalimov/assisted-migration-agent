package filter

import (
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
)

// sqlToString is a test helper that converts a Sqlizer to SQL with args interpolated.
func sqlToString(sqlizer sq.Sqlizer) (string, error) {
	sql, args, err := sqlizer.ToSql()
	if err != nil {
		return "", err
	}
	for _, arg := range args {
		var replacement string
		switch v := arg.(type) {
		case float64:
			replacement = fmt.Sprintf("%.2f", v)
		default:
			replacement = fmt.Sprintf("'%v'", arg)
		}
		sql = strings.Replace(sql, "?", replacement, 1)
	}
	return sql, nil
}
