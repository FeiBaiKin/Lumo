package main

import (
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/migrations"
)

// modules 返回编译进本二进制的功能模块，顺序即注册顺序，也决定迁移与启动顺序。
//
// 阶段 3 起的每个业务模块都在此登记；这是核心与模块之间唯一的装配点（agent.md §3.2）。
// 模块的 Register 只做装配，不得访问数据库：migrate 命令也会走同一条注册链，
// 而那时业务表可能尚不存在。需要读写库的启动逻辑放到 Start。
func modules() []app.Module {
	return []app.Module{
		taxonomy.New(),
	}
}

// migrationSources 汇总核心与各模块的迁移来源，核心永远最先执行。
//
// 每个来源拥有独立的版本表，模块之间的迁移编号互不干扰（见 internal/migrate）。
func migrationSources(application *app.App) []migrate.Source {
	collected := application.Migrations()
	sources := make([]migrate.Source, 0, 1+len(collected))
	sources = append(sources, migrate.Source{Name: migrate.CoreName, FS: migrations.FS})
	for _, m := range collected {
		sources = append(sources, migrate.Source{Name: m.Module, FS: m.FS})
	}
	return sources
}
