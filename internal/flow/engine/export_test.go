package engine

// SetAfterClaim 设置认领成功后、处理前的钩子，供测试构造交错。
func SetAfterClaim(e *Engine, f func(Job)) { e.afterClaim = f }
