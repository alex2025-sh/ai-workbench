// Package model 是数据访问层，由 goctl 根据 deploy/sql/*.sql 生成：
//
//	make gen-model
//
// 生成规则：
//   - *_model_gen.go 每次生成都会被覆盖，不要手工修改；
//   - *_model.go 只在首次生成，自定义查询（如 CAS 认领、按 seq 条件更新）写在这里；
//   - vars.go 定义 ErrNotFound 等公共变量。
package model
