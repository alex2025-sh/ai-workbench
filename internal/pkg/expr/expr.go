// Package expr 是 DSL 中条件、映射、模板表达式的编译与求值。
//
// 表达式在编译期预编译、运行时求值，必须是纯函数：不暴露 now()、随机数与外部 IO，
// 当前时间由引擎的 Clock 注入，保证推进过程可复现。实现可基于 expr-lang/expr 等沙箱化的库。
package expr

// Env 是求值环境，包含 trigger.payload.*、nodes.<id>.output.*、vars.*、loop.iteration 等命名空间。
type Env map[string]any

// Program 是预编译后的表达式。
type Program interface {
	Run(env Env) (any, error)
}

// Compiler 编译表达式；语法错误在编译期直接返回。
type Compiler interface {
	Compile(src string) (Program, error)
}
