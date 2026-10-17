package model

import "database/sql"

// affected 把 Exec 的结果转换为“是否恰好影响了一行”，用于 CAS 类更新。
func affected(ret sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := ret.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
