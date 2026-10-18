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

// truncate 把字符串截到 n 字节以内，用于写入有长度上限的列（如 last_error）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
