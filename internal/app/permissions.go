package app

import "github.com/FeiBaiKin/lumo/internal/auth/perm"

// CorePermissions 返回核心自身引入的权限声明（agent.md §7.2）。
//
// 与模块的 Permissions() 并列：用户、角色、主题与站点级操作由核心实现，
// 没有对应的功能模块，故在这里声明。角色编辑器据此拿到中文名，
// 缺了它这些权限会以「有权限串、没名字」的形态出现在界面上。
func CorePermissions() []Permission {
	return []Permission{
		{Key: perm.UsersManage.String(), Label: "管理用户", Description: "创建、修改、停用与删除用户"},
		{Key: perm.RolesManage.String(), Label: "管理角色", Description: "创建、修改与删除自定义角色"},
		{Key: perm.ThemesManage.String(), Label: "管理主题", Description: "上传、切换与配置主题"},
		{
			Key:   perm.ContentUnsafeHTML.String(),
			Label: "发布未净化的正文",
			Description: "正文中的 HTML 原样输出到前台（可含 iframe 与脚本）。" +
				"前台与后台同源，未净化的脚本能以访客身份调用管理接口，故只应授予完全可信的角色",
		},
		{Key: perm.SiteDelete.String(), Label: "删除站点", Description: "删除整个站点及其全部数据"},
		{Key: perm.SiteTransfer.String(), Label: "转移所有权", Description: "把站点所有权转移给其他用户"},
	}
}
