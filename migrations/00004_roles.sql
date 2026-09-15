-- +goose Up
-- 内置角色由五挡收成三挡（2026-09-15 站长定）：用户 / 编辑 / 管理员，
-- 外加不对站长开放的 super-admin。显示名与描述见 perm.BuiltinRoleMeta。
--
-- author 不再内置。仍被用户持有时，把它整条降级为同名的**自定义角色**，
-- 权限原样保留：
--   - 直接删行会经 user_roles 的 ON DELETE CASCADE 静默摘掉这些人的角色，
--     没有角色就是没有任何权限，他们会连前台账户页都进不去；
--   - 改挂 editor 是一次静默提权：editor 能发布、能删任何人的文章，
--     而 author 本来只能碰自己的内容。
-- 降级成自定义角色既保住这些账号眼下能做的事，又把它交回站长的界面
-- （自定义角色可改可删，不再随启动被播种覆盖）。
-- 没有持有者时直接删掉这条内置行，界面上就不会多出一个没人用的角色。
UPDATE roles
   SET builtin = false,
       label = '作者',
       description = '旧版内置角色：只能写、改、删自己的文章与附件，'
                     || '管自己文章下的评论，不能发布。新版内置角色只有用户 / 编辑 / 管理员三挡，'
                     || '这条保留下来给仍持有它的账号，可自行修改或删除'
 WHERE name = 'author'
   AND EXISTS (SELECT 1 FROM user_roles WHERE role_id = roles.id);

DELETE FROM roles WHERE name = 'author' AND builtin = true;

-- +goose Down
-- 不恢复 author 角色。它要么已被降级成自定义角色、要么已被删除，
-- 而「哪些自定义角色是本次降级的产物」已经无从分辨（用户可能已改过它的权限或名字）。
-- 需要重做这次判断时，重新执行一次 Up 即可。
SELECT 1;
