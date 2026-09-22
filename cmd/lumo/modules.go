package main

import (
	"github.com/FeiBaiKin/lumo/internal/account"
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/comment"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/extension"
	"github.com/FeiBaiKin/lumo/internal/favorite"
	"github.com/FeiBaiKin/lumo/internal/logs"
	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/media"
	"github.com/FeiBaiKin/lumo/internal/menu"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/plugin"
	"github.com/FeiBaiKin/lumo/internal/search"
	"github.com/FeiBaiKin/lumo/internal/seo"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/theme"
	"github.com/FeiBaiKin/lumo/internal/update"
	"github.com/FeiBaiKin/lumo/migrations"
)

// modules 返回编译进本二进制的功能模块，顺序即注册顺序，也决定迁移与启动顺序。
//
// 阶段 3 起的每个业务模块都在此登记；这是核心与模块之间唯一的装配点。
// 模块的 Register 只做装配，不得访问数据库：migrate 命令也会走同一条注册链，
// 而那时业务表可能尚不存在。需要读写库的启动逻辑放到 Start。
//
// 顺序即依赖：settings 与 mail 被其他模块经 App.Lookup 取用，须最先；
// media 读 storage 设置分组，排在 settings 之后；
// content 的迁移引用 taxonomy 的表，必须排在它之后；
// comment 引用 content 的 posts 表；favorite 也引用它，且必须排在 theme 之前——
// 主题在装配期就要取到收藏查询，用来渲染收藏页与文章页上的收藏按钮；
// menu 与 seo 读取 content、taxonomy 与 users 的表；
// extension 只碰核心的 extensions 表，不依赖任何模块；
// search 给 content 的 posts 表加索引列，须排在 content 之后，也须在 theme 之前——
// 前台搜索页要在装配期取到它；
// theme 读取以上全部模块的表来渲染前台，排在最后；
// account 用 theme 的 Renderer 渲染账户页，故排在它之后；
// logs 与 update 都不依赖任何模块，位置随意，放在前面是为了它们的接口在规范里挨着 settings。
func modules() []app.Module {
	return []app.Module{
		settings.New(),
		mail.New(),
		// logs 只读配置里的日志目录，不依赖任何模块，也不碰数据库。
		logs.New(),
		// update 只碰自己的二进制与 data/backups，同样不依赖任何模块。
		update.New(),
		media.New(),
		taxonomy.New(),
		content.New(),
		comment.New(),
		favorite.New(),
		menu.New(),
		seo.New(),
		extension.New(),
		// plugin 只碰自己的 plugins 表与文件系统，不依赖任何模块；
		// 排在 extension 之后是为将来：声明式页面要往 Extension 平面放自定义模型。
		plugin.New(),
		search.New(),
		theme.New(),
		account.New(),
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
