package main

import (
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/comment"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/media"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/migrations"
)

// modules 返回编译进本二进制的功能模块，顺序即注册顺序，也决定迁移与启动顺序。
//
// 阶段 3 起的每个业务模块都在此登记；这是核心与模块之间唯一的装配点（agent.md §3.2）。
// 模块的 Register 只做装配，不得访问数据库：migrate 命令也会走同一条注册链，
// 而那时业务表可能尚不存在。需要读写库的启动逻辑放到 Start。
//
// 顺序即依赖：settings 与 mail 被其他模块经 App.Lookup 取用，须最先；
// media 读 storage 设置分组，排在 settings 之后；
// content 的迁移引用 taxonomy 的表，必须排在它之后；
// comment 引用 content 的 posts 表，排在最后。
func modules() []app.Module {
	return []app.Module{
		settings.New(),
		mail.New(),
		media.New(),
		taxonomy.New(),
		content.New(),
		comment.New(),
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
