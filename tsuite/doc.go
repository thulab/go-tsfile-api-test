// Package tsuite 承载 RN-145（TsFile Go 接口）的 API 场景用例。
//
// 约定：一个用例 = 一个子测试，子测试名即 RN-145 用例编号（形如 path_空字符串），
// 通过 `go test -run 'TestNewWriter/path_空字符串'` 可单独执行一条。
package tsuite
