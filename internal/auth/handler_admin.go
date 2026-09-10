package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth/password"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// tagUsers 与 tagRoles 是 OpenAPI 分组标签。
var (
	tagUsers = []string{"users"}
	tagRoles = []string{"roles"}
)

// 用户与角色管理的路由路径。
const (
	pathUsers        = "/users"
	pathUserByID     = "/users/{id}"
	pathUserStatus   = "/users/{id}/status"
	pathUserRoles    = "/users/{id}/roles"
	pathUserPassword = "/users/{id}/password"
	pathRoles        = "/roles"
	pathRoleByID     = "/roles/{id}"
)

// PermissionInfo 是一条权限的展示信息，由各模块声明、核心汇总。
type PermissionInfo struct {
	Key         string
	Label       string
	Description string
}

// AdminHandler 提供 Console 的用户与角色管理端点。
//
// 与 Handler 分开是因为二者的依赖与关注点都不同：认证端点只需要会话与令牌，
// 管理端点需要用户存储与密码校验，且权限要求完全不同。
type AdminHandler struct {
	store   *Store
	service *Service
	// perms 延迟取权限清单：模块在注册期声明权限，而本处理器在模块之前构造。
	perms func() []PermissionInfo
}

// NewAdminHandler 构造 AdminHandler；perms 可为 nil，此时权限清单只有权限串本身。
func NewAdminHandler(store *Store, service *Service, perms func() []PermissionInfo) *AdminHandler {
	return &AdminHandler{store: store, service: service, perms: perms}
}

// Register 挂载管理端点，全部要求 users:manage 或 roles:manage。
func (h *AdminHandler) Register(console huma.API) {
	users := huma.Middlewares{RequirePermission(perm.UsersManage)}
	roles := huma.Middlewares{RequirePermission(perm.RolesManage)}

	// ---- 用户 ----
	huma.Register(console, huma.Operation{
		OperationID: "user-page",
		Method:      http.MethodGet,
		Path:        pathUsers,
		Summary:     "分页列出用户",
		Description: "按创建时间倒序，可按角色、启用状态与关键词筛选。响应不含口令哈希。",
		Tags:        tagUsers,
		Middlewares: users,
	}, h.pageUsers)
	huma.Register(console, huma.Operation{
		OperationID: "user-get",
		Method:      http.MethodGet,
		Path:        pathUserByID,
		Summary:     "获取用户",
		Tags:        tagUsers,
		Middlewares: users,
		Errors:      []int{http.StatusNotFound},
	}, h.getUser)
	huma.Register(console, huma.Operation{
		OperationID:   "user-create",
		Method:        http.MethodPost,
		Path:          pathUsers,
		Summary:       "创建用户",
		Description:   "口令由调用方一次性给出，服务端只存哈希；响应与日志都不回显口令。",
		Tags:          tagUsers,
		DefaultStatus: http.StatusCreated,
		Middlewares:   users,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, h.createUser)
	huma.Register(console, huma.Operation{
		OperationID: "user-update",
		Method:      http.MethodPut,
		Path:        pathUserByID,
		Summary:     "更新用户资料",
		Description: "改邮箱、显示名、头像与简介；不动口令与角色。",
		Tags:        tagUsers,
		Middlewares: users,
		Errors:      []int{http.StatusBadRequest, http.StatusConflict, http.StatusNotFound},
	}, h.updateUser)
	huma.Register(console, huma.Operation{
		OperationID: "user-set-status",
		Method:      http.MethodPut,
		Path:        pathUserStatus,
		Summary:     "启用或停用用户",
		Description: "停用会立即清除该用户的全部会话与令牌。不能停用自己，也不能停用最后一名管理员。",
		Tags:        tagUsers,
		Middlewares: users,
		Errors:      []int{http.StatusConflict, http.StatusNotFound},
	}, h.setUserStatus)
	huma.Register(console, huma.Operation{
		OperationID: "user-set-roles",
		Method:      http.MethodPut,
		Path:        pathUserRoles,
		Summary:     "设置用户角色",
		Description: "整体替换角色集合。不能移除自己的最后一个管理角色，也不能移除最后一名管理员的管理角色。",
		Tags:        tagUsers,
		Middlewares: users,
		Errors:      []int{http.StatusBadRequest, http.StatusConflict, http.StatusNotFound},
	}, h.setUserRoles)
	huma.Register(console, huma.Operation{
		OperationID: "user-reset-password",
		Method:      http.MethodPut,
		Path:        pathUserPassword,
		Summary:     "重置用户口令",
		Description: "设置新口令并清除该用户全部会话与令牌；响应不回显口令。",
		Tags:        tagUsers,
		Middlewares: users,
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.resetPassword)
	huma.Register(console, huma.Operation{
		OperationID:   "user-delete",
		Method:        http.MethodDelete,
		Path:          pathUserByID,
		Summary:       "删除用户",
		Description:   "不能删除自己，也不能删除最后一名管理员。用户发布过的内容因外键约束会阻止删除。",
		Tags:          tagUsers,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   users,
		Errors:        []int{http.StatusConflict, http.StatusNotFound},
	}, h.deleteUser)

	// ---- 权限清单 ----

	// ---- 角色 ----
	huma.Register(console, huma.Operation{
		OperationID: "role-list",
		Method:      http.MethodGet,
		Path:        pathRoles,
		Summary:     "列出角色",
		Description: "含内置与自定义角色。内置角色每次启动以代码为准覆盖，不可改删。",
		Tags:        tagRoles,
		Middlewares: roles,
	}, h.listRoles)
	huma.Register(console, huma.Operation{
		OperationID:   "role-create",
		Method:        http.MethodPost,
		Path:          pathRoles,
		Summary:       "创建自定义角色",
		Description:   "权限串须在 v1 清单内；拼错会被拒绝，不会静默变成一条无效权限。",
		Tags:          tagRoles,
		DefaultStatus: http.StatusCreated,
		Middlewares:   roles,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, h.createRole)
	huma.Register(console, huma.Operation{
		OperationID: "role-update",
		Method:      http.MethodPut,
		Path:        pathRoleByID,
		Summary:     "更新自定义角色",
		Tags:        tagRoles,
		Middlewares: roles,
		Errors:      []int{http.StatusBadRequest, http.StatusConflict, http.StatusNotFound},
	}, h.updateRole)
	huma.Register(console, huma.Operation{
		OperationID:   "role-delete",
		Method:        http.MethodDelete,
		Path:          pathRoleByID,
		Summary:       "删除自定义角色",
		Description:   "仍被用户持有时拒绝删除；内置角色不可删。",
		Tags:          tagRoles,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   roles,
		Errors:        []int{http.StatusConflict, http.StatusNotFound},
	}, h.deleteRole)

	// 权限清单：前端渲染角色编辑器时的可选项。
	huma.Register(console, huma.Operation{
		OperationID: "role-permissions",
		Method:      http.MethodGet,
		Path:        "/permissions",
		Summary:     "列出全部权限串",
		Description: "v1 权限清单，供角色编辑器渲染可勾选项。",
		Tags:        tagRoles,
		Middlewares: roles,
	}, h.listPermissions)
}

// ---------- 输入输出 ----------

type userIDInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type userPageInput struct {
	api.PageParams
	Role   string `query:"role" maxLength:"64" doc:"按角色名筛选"`
	Status string `query:"status" enum:"enabled,disabled" doc:"按启用状态筛选"`
	Q      string `query:"q" maxLength:"200" doc:"按用户名、邮箱或显示名模糊筛选"`
}

type userPageOutput struct {
	Body api.Page[User]
}

type userOutput struct {
	Body User
}

type createUserBody struct {
	Username    string   `json:"username" minLength:"3" maxLength:"32" pattern:"^[a-zA-Z0-9][a-zA-Z0-9_.-]*$"`
	Email       string   `json:"email" format:"email" maxLength:"254"`
	Password    string   `json:"password" minLength:"8" maxLength:"128" doc:"一次性给出，服务端只存哈希"`
	DisplayName string   `json:"displayName,omitempty" maxLength:"64"`
	Roles       []string `json:"roles,omitempty" doc:"角色名列表，至少一个"`
}

type createUserInput struct {
	Body createUserBody
}

type updateUserBody struct {
	Email       string `json:"email" format:"email" maxLength:"254"`
	DisplayName string `json:"displayName,omitempty" maxLength:"64"`
	AvatarURL   string `json:"avatarUrl,omitempty" maxLength:"1024"`
	Bio         string `json:"bio,omitempty" maxLength:"500"`
}

type updateUserInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body updateUserBody
}

type statusBody struct {
	Disabled bool `json:"disabled" doc:"true 为停用"`
}

type statusInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body statusBody
}

type rolesBody struct {
	Roles []string `json:"roles" doc:"角色名列表，整体替换"`
}

type rolesInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body rolesBody
}

type passwordBody struct {
	Password string `json:"password" minLength:"8" maxLength:"128"`
}

type passwordInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body passwordBody
}

type roleIDInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type roleBody struct {
	Name        string   `json:"name,omitempty" minLength:"1" maxLength:"64" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"角色名，创建后不可改"`
	Label       string   `json:"label,omitempty" maxLength:"64"`
	Description string   `json:"description,omitempty" maxLength:"500"`
	Permissions []string `json:"permissions" doc:"权限串列表，须在 v1 清单内"`
}

type createRoleInput struct {
	Body roleBody
}

type updateRoleInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body roleBody
}

type roleOutput struct {
	Body Role
}

type roleList struct {
	Items []Role `json:"items"`
}

type roleListOutput struct {
	Body roleList
}

type permissionView struct {
	Key         string `json:"key"`
	DisplayName string `json:"label" doc:"权限的显示名；模块未声明时回退为权限串本身"`
	Description string `json:"description"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	// IsAny 为真表示该权限不限所有权。
	IsAny bool `json:"isAny"`
}

type permissionList struct {
	Items []permissionView `json:"items"`
}

type permissionListOutput struct {
	Body permissionList
}

// ---------- 用户处理器 ----------

func (h *AdminHandler) pageUsers(ctx context.Context, in *userPageInput) (*userPageOutput, error) {
	items, total, err := h.store.PageUsers(ctx, UserFilter{
		Role: in.Role, Status: in.Status, Q: in.Q,
	}, in.PageParams)
	if err != nil {
		return nil, err
	}
	return &userPageOutput{Body: api.NewPage(items, in.PageParams, total)}, nil
}

func (h *AdminHandler) getUser(ctx context.Context, in *userIDInput) (*userOutput, error) {
	u, err := h.store.GetUser(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	return &userOutput{Body: *u}, nil
}

func (h *AdminHandler) createUser(ctx context.Context, in *createUserInput) (*userOutput, error) {
	if len(in.Body.Roles) == 0 {
		return nil, huma.Error400BadRequest("至少指定一个角色")
	}
	if err := h.checkRoles(ctx, in.Body.Roles); err != nil {
		return nil, err
	}
	if err := password.Validate(in.Body.Password); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	u, err := h.store.CreateUser(ctx, &CreateUserParams{
		Username:    in.Body.Username,
		Email:       in.Body.Email,
		Password:    in.Body.Password,
		DisplayName: in.Body.DisplayName,
		Roles:       in.Body.Roles,
	})
	if err != nil {
		return nil, mapAdminError(err)
	}
	return &userOutput{Body: *u}, nil
}

func (h *AdminHandler) updateUser(ctx context.Context, in *updateUserInput) (*userOutput, error) {
	if _, err := h.store.GetUser(ctx, in.ID); err != nil {
		return nil, mapAdminError(err)
	}
	if err := h.store.UpdateUserProfile(ctx, in.ID,
		in.Body.Email, in.Body.DisplayName, in.Body.AvatarURL, in.Body.Bio); err != nil {
		return nil, mapAdminError(err)
	}
	u, err := h.store.GetUser(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	return &userOutput{Body: *u}, nil
}

func (h *AdminHandler) setUserStatus(ctx context.Context, in *statusInput) (*userOutput, error) {
	u, err := h.store.GetUser(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	if in.Body.Disabled {
		if guardErr := h.guardLastAdmin(ctx, u, "停用"); guardErr != nil {
			return nil, guardErr
		}
		if guardErr := h.guardSelf(ctx, u, "停用"); guardErr != nil {
			return nil, guardErr
		}
		// 停用走服务层：它会连带清除该用户的全部会话与令牌。
		if disableErr := h.service.Disable(ctx, u.ID); disableErr != nil {
			return nil, mapAdminError(disableErr)
		}
	} else if enableErr := h.store.SetDisabled(ctx, u.ID, false); enableErr != nil {
		return nil, mapAdminError(enableErr)
	}

	updated, err := h.store.GetUser(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	return &userOutput{Body: *updated}, nil
}

func (h *AdminHandler) setUserRoles(ctx context.Context, in *rolesInput) (*userOutput, error) {
	u, err := h.store.GetUser(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	if len(in.Body.Roles) == 0 {
		return nil, huma.Error400BadRequest("至少保留一个角色")
	}
	if roleErr := h.checkRoles(ctx, in.Body.Roles); roleErr != nil {
		return nil, roleErr
	}

	// 把某人的管理角色摘掉，可能让站点失去最后一名管理员。
	wasAdmin := HasAdminRole(u.RoleNames())
	if wasAdmin && !HasAdminRole(in.Body.Roles) {
		if guardErr := h.guardLastAdmin(ctx, u, "移除管理员角色"); guardErr != nil {
			return nil, guardErr
		}
		if guardErr := h.guardSelf(ctx, u, "移除自己的管理员角色"); guardErr != nil {
			return nil, guardErr
		}
	}

	if assignErr := h.store.AssignRoles(ctx, u.ID, in.Body.Roles); assignErr != nil {
		return nil, mapAdminError(assignErr)
	}
	updated, err := h.store.GetUser(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	return &userOutput{Body: *updated}, nil
}

func (h *AdminHandler) resetPassword(ctx context.Context, in *passwordInput) (*userOutput, error) {
	u, err := h.store.GetUser(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	if err := password.Validate(in.Body.Password); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	// 重置走服务层：它会清除该用户全部会话与令牌。
	if err := h.service.ResetPassword(ctx, u.ID, in.Body.Password); err != nil {
		return nil, mapAdminError(err)
	}
	return &userOutput{Body: *u}, nil
}

func (h *AdminHandler) deleteUser(ctx context.Context, in *userIDInput) (*struct{}, error) {
	u, err := h.store.GetUser(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	if err := h.guardLastAdmin(ctx, u, "删除"); err != nil {
		return nil, err
	}
	if err := h.guardSelf(ctx, u, "删除"); err != nil {
		return nil, err
	}
	if err := h.store.DeleteUser(ctx, u.ID); err != nil {
		return nil, mapAdminError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// ---------- 角色处理器 ----------

func (h *AdminHandler) listRoles(ctx context.Context, _ *struct{}) (*roleListOutput, error) {
	items, err := h.store.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	return &roleListOutput{Body: roleList{Items: items}}, nil
}

func (h *AdminHandler) createRole(ctx context.Context, in *createRoleInput) (*roleOutput, error) {
	permissions, err := parsePermissions(in.Body.Permissions)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Body.Name)
	if name == "" {
		return nil, huma.Error400BadRequest("请填写角色名")
	}

	role := &Role{
		Name:        name,
		Label:       strings.TrimSpace(in.Body.Label),
		Description: strings.TrimSpace(in.Body.Description),
		Permissions: permissions,
	}
	if err := h.store.CreateRole(ctx, role); err != nil {
		return nil, mapAdminError(err)
	}
	return &roleOutput{Body: *role}, nil
}

func (h *AdminHandler) updateRole(ctx context.Context, in *updateRoleInput) (*roleOutput, error) {
	permissions, err := parsePermissions(in.Body.Permissions)
	if err != nil {
		return nil, err
	}
	if updateErr := h.store.UpdateRole(ctx, in.ID,
		in.Body.Label, in.Body.Description, permissions); updateErr != nil {
		return nil, mapAdminError(updateErr)
	}
	role, err := h.store.roleByID(ctx, in.ID)
	if err != nil {
		return nil, mapAdminError(err)
	}
	return &roleOutput{Body: *role}, nil
}

func (h *AdminHandler) deleteRole(ctx context.Context, in *roleIDInput) (*struct{}, error) {
	if err := h.store.DeleteRole(ctx, in.ID); err != nil {
		return nil, mapAdminError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// listPermissions 返回全部权限串及其展示信息。
//
// 以 perm.All 为准、模块与核心声明的标签为辅：漏声明时权限仍然出现在清单里，
// 只是显示权限串本身——隐藏一条真实存在的权限，比显示一个没有中文名的选项危险得多。
func (h *AdminHandler) listPermissions(_ context.Context, _ *struct{}) (*permissionListOutput, error) {
	labels := map[string]PermissionInfo{}
	if h.perms != nil {
		for _, info := range h.perms() {
			labels[info.Key] = info
		}
	}

	items := make([]permissionView, 0, len(perm.All))
	for _, p := range perm.All {
		view := permissionView{
			Key:         p.String(),
			DisplayName: p.String(),
			Resource:    p.Resource(),
			Action:      p.Action(),
			IsAny:       p.IsAny(),
		}
		if info, ok := labels[p.String()]; ok {
			view.DisplayName = info.Label
			view.Description = info.Description
		}
		items = append(items, view)
	}
	return &permissionListOutput{Body: permissionList{Items: items}}, nil
}

// ---------- 工具 ----------

// guardLastAdmin 拒绝会让站点失去最后一名管理员的操作。
//
// 这类自锁没有后门可走：一旦最后一名管理员被停用或删除，只能改库才能恢复。
func (h *AdminHandler) guardLastAdmin(ctx context.Context, u *User, action string) error {
	if !HasAdminRole(u.RoleNames()) {
		return nil
	}
	count, err := h.store.CountAdmins(ctx)
	if err != nil {
		return err
	}
	if count <= 1 {
		return huma.Error409Conflict(ErrLastAdmin.Error() + "：" + action + "后站点将无人可管理")
	}
	return nil
}

// guardSelf 拒绝作用在自己身上的危险操作。
func (h *AdminHandler) guardSelf(ctx context.Context, u *User, action string) error {
	if p, ok := FromContext(ctx); ok && p.UserID() == u.ID {
		return huma.Error409Conflict(ErrSelfOperation.Error() + "：" + action)
	}
	return nil
}

// checkRoles 校验给定的角色名都存在。
func (h *AdminHandler) checkRoles(ctx context.Context, names []string) error {
	for _, name := range names {
		if _, err := h.store.RoleByName(ctx, name); err != nil {
			if errors.Is(err, ErrRoleNotFound) {
				return huma.Error400BadRequest("角色不存在：" + name)
			}
			return err
		}
	}
	return nil
}

// parsePermissions 校验并转换权限串。
//
// 拼错的权限串会被明确拒绝，而不是静默变成一条永远不会命中的权限——
// 后者会让一次配置错误表现为难以排查的 403。
func parsePermissions(keys []string) ([]perm.Permission, error) {
	out := make([]perm.Permission, 0, len(keys))
	for _, key := range keys {
		p, err := perm.Parse(key)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		out = append(out, p)
	}
	return out, nil
}

// mapAdminError 把存储层的哨兵错误映射为 HTTP 错误；其余错误交给 huma 按 500 处理。
func mapAdminError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrUserNotFound), errors.Is(err, ErrRoleNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrDuplicate):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrLastAdmin), errors.Is(err, ErrSelfOperation),
		errors.Is(err, ErrRoleInUse), errors.Is(err, ErrBuiltinRole):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrUserHasContent):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	}
	return err
}
