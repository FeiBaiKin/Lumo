// Package migrations 以 embed 方式携带核心 SQL 迁移。
//
// 迁移随二进制分发，部署时无需附带 SQL 文件（agent.md §9「零运维升级」）。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
