// Package media 提供附件与图片处理（agent.md §8）。
//
// 上传的文件经 Storage 接口落到本地目录或 S3 兼容对象存储；图片额外生成多档 WebP 缩略图。
// 存储位置由 storage 设置分组决定，可在运行时切换；S3 密钥只从环境变量读取，不入库。
package media

import (
	"embed"
	"io/fs"
	"log/slog"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_media）。
const Name = "media"

// Module 是附件模块。
type Module struct {
	store   *Store
	service *Service
	logger  *slog.Logger
}

// New 构造模块。
func New() *Module {
	return &Module{}
}

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
func (m *Module) Register(a *app.App) error {
	m.logger = a.Logger()
	cfg := a.Config()
	if db := a.DB(); db != nil {
		m.store = NewStore(db.DB, auth.NewStore(db.DB))
	}
	m.service = NewService(ServiceOptions{
		Store:     m.store,
		Settings:  settings.From(a),
		DataDir:   cfg.DataDir,
		Logger:    m.logger,
		MaxUpload: cfg.Server.MaxUploadSize,
	})
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("media: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Settings 实现 app.SettingsProvider。
func (m *Module) Settings() []app.SettingGroup {
	return []app.SettingGroup{storageGroup()}
}

// Permissions 实现 app.PermissionProvider。
func (m *Module) Permissions() []app.Permission {
	return []app.Permission{
		{Key: perm.MediaWrite.String(), Label: "上传附件", Description: "上传附件并修改自己上传的附件"},
		{Key: perm.MediaDeleteAny.String(), Label: "管理任何附件", Description: "修改或删除他人上传的附件"},
	}
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.store == nil {
		return
	}
	NewHandler(m.service, m.store).Register(r.Console())
}

// Service 返回上传服务，供其他模块（如主题打包、导入导出）复用。
func (m *Module) Service() *Service { return m.service }
