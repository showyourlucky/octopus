# 根目录工作记忆

## build.bat

- 菜单选择与前端构建确认统一使用 `set /p` 读取，避免 `choice` 在当前终端环境下出现分支误判。
- 前端构建确认规则：输入 `n` 或 `N` 时跳过前端构建，其余输入与直接回车都按构建处理。
- 批处理文件必须使用 UTF-8（无 BOM）+ CRLF 换行；仅 LF 换行会导致 `cmd.exe` 串行解析，出现标签缺失或乱码命令。
- 文件开头必须先完成路径初始化、变量配置、日志函数定义，并通过 `goto :main` 进入主流程，避免从第一个业务标签顺序执行。
- 菜单模式依赖 `:execute_build`、`:execute_release`、`:menu_return`、`:exit_script` 标签；新增菜单分支时必须同步补齐目标标签。
- 当前菜单支持：
  - `1`：仅构建 `windows/amd64`
  - `2`：仅构建 `linux/arm64`
  - `3`：执行发布构建
  - `0`：退出脚本

## 分组接口

- `POST /api/v1/group/create` 与分组改名场景遇到重复 `groups.name` 唯一约束时，需要返回中文可读提示：`分组名称已存在，请使用其他名称`，不要把底层 SQLite 约束错误直接透传给前端。

## 快速创建分组（group-template）

- `GET /api/v1/group-template/ungrouped-models`：获取所有渠道中未分组的模型列表
- `POST /api/v1/group-template/batch-create`：批量创建分组，每个分组独立配置模式和排除字段
- 正则生成规则（前端完成）：模型名以 `-`、`_`、`.` 分割，第一段=前缀，剩余=后缀，后缀中的版本号分隔符（如 `5.5`）转换为 `[._-]` 字符类，模板：`(?i)^(?!.*(排除1|排除2)).*前缀.*后缀.*$`
- 后端不生成正则：`match_regex` 为空则跳过匹配，非空则直接编译使用
- 全局开关：工具栏 Switch 控制所有分组是否自动应用正则
- 前端入口：分组页面工具栏的 Zap 图标按钮
- 事务保护：`batch-create` 使用 `op.GroupBatchCreate` 在单个事务中批量创建，任一失败则整体回滚
- ReDoS 防护：用户输入正则长度限制 512 字符（`regexMaxLen`），超长直接拒绝
- 正则 UI：复合输入框设计，左侧不可编辑区（`(?i)` + 排除词正则，灰底锁定）+ 右侧可编辑区（核心正则，可点击编辑），视觉上为同一编辑框
- 匹配预览：`ModelConfigCard` 内通过 `useMatchPreview` hook 实时匹配全量模型，显示匹配计数 + Popover 查看列表
- 正则组装：统一使用 `buildFullRegex(name, useAutoRegex, matchRegex, exclude)` 函数，`(?i)` + `buildExcludeRegex(exclude)` + 核心正则（用户自定义或 `generateCoreRegex` 自动生成），`handleBatchCreate` 与 `ModelConfigCard` 预览逻辑共用同一函数
- 前端性能：`ModelConfigCard` 使用 `React.memo`、`useMemo` 缓存正则计算和匹配预览、模型列表超过 50 条时启用 `@tanstack/react-virtual` 虚拟滚动

## 日志筛选与错误溯源

- 已合并 PR #194：日志筛选增强（后端筛选 + 可搜索下拉），支持按分组、模型、是否重试、渠道过滤
- `ChannelAttempt` 新增 `channel_key_remark` 字段，relay 记录尝试时自动写入渠道 Key 的备注
- 日志详情的尝试记录中显示 `[备注名]` 标识，方便识别具体是哪个渠道 Key 出了问题
- `ChannelKey.Remark` 为空时该字段为空字符串，前端仅在非空时显示

## API Key 模型访问类型

- `APIKey.ModelAccessType` 使用 `model_access_type` 序列化，默认 `group`，旧 Key 和空值必须保持只查询/请求分组模型。
- `all_channel` 类型允许 `/v1/models` 返回分组模型与启用渠道配置模型的去重集合；禁用渠道模型不能出现在 API Key 可见列表或白名单候选中，请求时仍优先匹配真实分组。
- `supported_models` 继续作为白名单：为空表示当前访问类型范围内不限制，非空时模型列表和请求都必须过滤。
- 分组外模型只在 `all_channel` 类型下按模型名精确匹配启用渠道，不自动写入分组，也不改变分组页面数据结构。
