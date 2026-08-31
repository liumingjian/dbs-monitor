import {
  Button,
  ContentSwitcher,
  CopyButton,
  Select,
  SelectItem,
  Switch,
  TextInput,
} from '@carbon/react'
import { createRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import type { FieldPath, UseFormRegisterReturn } from 'react-hook-form'
import { z } from 'zod'
import { $api } from '../../api/client'
import { apiErrorMessage, applyApiFieldErrors } from '../../api/errors'
import type { components } from '../../api/schema'
import { zodResolver } from '../../forms/zodResolver'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import { Modal } from '../../primitives/Modal'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Pagination } from '../../primitives/Pagination'
import { Panel } from '../../primitives/Panel'
import { StatusDot } from '../../primitives/StatusDot'
import { TruncatedText } from '../../primitives/TruncatedText'
import { rootRoute } from '../root'
import { browserStorage } from '../root/navCollapse'
import type { TableDensity } from '../root/tableDensity'
import { densityLabel, readTableDensity, writeTableDensity } from '../root/tableDensity'
import './users.css'

type Role = components['schemas']['Role']
type User = components['schemas']['User']
type UserCreated = components['schemas']['UserCreated']
type UserCreateInput = components['schemas']['UserCreateInput']
type UserRoleInput = components['schemas']['UserRoleInput']
type IssuedPassword = { title: string; password: string }

const roles = ['READONLY', 'ALERT_ADMIN', 'PLATFORM_ADMIN'] as const satisfies readonly Role[]

export const usersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/users',
  component: UsersPage,
})

/// 用户管理。
///
/// 版式照列表页样板（web/CLAUDE.md 先例）：页头（`h1` + 唯一的主操作）、一个 `flush` 的
/// 面板包住 `DataGrid`、分页放进面板 footer、密度切换放在面板标题栏右侧。这一页没有筛选，
/// 所以样板三段里的工具条那一段不渲染 —— 空着一条工具条不比没有它多说明任何事。
///
/// 平台用户**只停用、不删除**（CONTEXT.md：停用之后已记录的操作仍然归属得上），
/// 所以行内那个按钮从头到尾叫「停用」，页面里没有任何一处写「删除」。
function UsersPage() {
  const usersQuery = $api.useQuery('get', '/api/v1/users')
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const updateStatusMutation = $api.useMutation('put', '/api/v1/users/{id}/status')
  const resetPasswordMutation = $api.useMutation('post', '/api/v1/users/{id}/password')
  const [createOpen, setCreateOpen] = useState(false)
  const [roleTarget, setRoleTarget] = useState<User | null>(null)
  const [issuedPassword, setIssuedPassword] = useState<IssuedPassword | null>(null)
  const [actionError, setActionError] = useState('')
  const [density, setDensity] = useState<TableDensity>(() => readTableDensity(browserStorage))
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const canManageUsers = currentUserQuery.data?.role === 'PLATFORM_ADMIN'

  const users = usersQuery.data ?? []
  // 数据变少之后停在一个空页上，看起来和「没有用户」一样，所以夹住页码。
  const lastPage = Math.max(1, Math.ceil(users.length / pageSize))
  const currentPage = Math.min(page, lastPage)
  const pageUsers = users.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  function refreshUsers() {
    setActionError('')
    void usersQuery.refetch()
  }

  function reportError(error: unknown) {
    setActionError(apiErrorMessage(error, '操作失败，请重试'))
  }

  function changeDensity(next: TableDensity) {
    setDensity(next)
    writeTableDensity(browserStorage, next)
  }

  function toggleUserStatus(user: User) {
    updateStatusMutation.mutate({ params: { path: { id: user.id } }, body: { enabled: !user.enabled } }, {
      onSuccess: refreshUsers,
      onError: reportError,
    })
  }

  function resetUserPassword(user: User) {
    setActionError('')
    resetPasswordMutation.mutate({ params: { path: { id: user.id } } }, {
      onSuccess: (result) => setIssuedPassword({ title: `${user.username} 的重置口令`, password: result.password }),
      onError: reportError,
    })
  }

  // 只读用户拿到的是**禁用的按钮加一句原因**，不是点下去才报错的按钮。
  const manageDisabledReason = canManageUsers ? undefined : '需要平台管理员角色'

  return (
    <div className="users-page">
      <header className="users-page__header">
        <h1 className="dbs-page-title">用户管理</h1>
        <span title={manageDisabledReason}>
          <Button size="md" renderIcon={Icon.glyph.add} disabled={!canManageUsers} onClick={() => setCreateOpen(true)}>
            创建用户
          </Button>
        </span>
      </header>

      {usersQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(usersQuery.error, '用户列表加载失败')} />
      )}
      {actionError !== '' && (
        <NotificationBar tone="critical" title={actionError} onClose={() => setActionError('')} />
      )}

      <Panel
        flush
        title={`用户（${users.length}）`}
        actions={<DensitySwitcher density={density} onChange={changeDensity} />}
        footer={<Pagination
          className="users-pagination"
          size="md"
          page={currentPage}
          pageSize={pageSize}
          pageSizes={[10, 25, 50]}
          totalItems={users.length}
          onChange={({ page: nextPage, pageSize: nextPageSize }) => {
            setPage(nextPage)
            setPageSize(nextPageSize)
          }}
        />}
      >
        <DataGrid<User>
          label="用户列表"
          density={density}
          loading={usersQuery.isPending}
          skeletonRows={6}
          rows={pageUsers}
          rowKey={(user) => user.id}
          rowTestId="user-row"
          columns={userColumns({
            currentUserID: currentUserQuery.data?.id,
            manageDisabledReason,
            resetPending: resetPasswordMutation.isPending,
            statusPending: updateStatusMutation.isPending,
            onToggleStatus: toggleUserStatus,
            onChangeRole: setRoleTarget,
            onResetPassword: resetUserPassword,
          })}
          empty={{ title: '还没有用户', description: '创建第一个平台用户，让它接手控制台。' }}
        />
      </Panel>

      <CreateUserModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(created) => {
          setCreateOpen(false)
          setIssuedPassword({ title: `${created.user.username} 的初始口令`, password: created.initial_password })
          refreshUsers()
        }}
      />
      {/* 角色对话框的默认值取自被点的那一行，所以按行挂载：`key` 一换表单就是新的，
          不必再写一个 effect 把上一行的选择洗掉。 */}
      {roleTarget !== null && (
        <RoleChangeModal
          key={roleTarget.id}
          user={roleTarget}
          onClose={() => setRoleTarget(null)}
          onSaved={() => {
            setRoleTarget(null)
            refreshUsers()
          }}
        />
      )}
      <OneTimePasswordModal issued={issuedPassword} onClose={() => setIssuedPassword(null)} />
    </div>
  )
}

/// 密度切换。产品级偏好，读写只有 `routes/root/tableDensity.ts` 一个去处（web/CLAUDE.md）。
function DensitySwitcher({ density, onChange }: { density: TableDensity; onChange: (density: TableDensity) => void }) {
  const densities = ['standard', 'dense'] as const satisfies readonly TableDensity[]
  return (
    <ContentSwitcher
      className="users-density"
      size="sm"
      selectedIndex={densities.indexOf(density)}
      onChange={({ index }) => {
        // 拿不到下标就是没换档，什么都不做，不要兜底成第一档。
        const next = index === undefined ? undefined : densities[index]
        if (next !== undefined) onChange(next)
      }}
    >
      {densities.map((value) => <Switch key={value} name={value} text={densityLabel(value)} />)}
    </ContentSwitcher>
  )
}

type UserColumnHandlers = {
  currentUserID: string | undefined
  manageDisabledReason: string | undefined
  resetPending: boolean
  statusPending: boolean
  onToggleStatus: (user: User) => void
  onChangeRole: (user: User) => void
  onResetPassword: (user: User) => void
}

/// 列定义。只给 `minWidth`，横向行为整个由 `primitives/DataGrid` 负责；一格一个事实。
function userColumns(handlers: UserColumnHandlers): DataGridColumn<User>[] {
  return [
    {
      key: 'username',
      header: '用户名',
      minWidth: 180,
      grow: 1.3,
      cell: (user) => <TruncatedText className="users-table__name">{user.username}</TruncatedText>,
    },
    {
      key: 'role',
      header: '角色',
      minWidth: 132,
      // 「这个角色能做什么」是这一列真正要回答的问题，一行里写不下，所以走单元格的
      // 悬停提示 —— 和其它列的截断提示是同一个机制，不是新发明的一种浮层。
      cell: (user) => (
        <TruncatedText title={`${roleLabel(user.role)}：${roleCapability(user.role)}`}>
          {roleLabel(user.role)}
        </TruncatedText>
      ),
    },
    {
      key: 'enabled',
      header: '状态',
      minWidth: 96,
      grow: 1.4,
      cell: (user) => (
        <StatusDot tone={user.enabled ? 'normal' : 'unknown'}>{user.enabled ? '启用' : '停用'}</StatusDot>
      ),
    },
    {
      key: 'created_at',
      header: '创建时间',
      minWidth: 176,
      // 时间戳有二十来个字符，1280px 下不多给一点就只剩日期。
      grow: 1.25,
      numeric: true,
      cell: (user) => <TruncatedText>{new Date(user.created_at).toLocaleString()}</TruncatedText>,
    },
    {
      key: 'actions',
      header: '操作',
      minWidth: 264,
      grow: 1.2,
      align: 'end',
      cell: (user) => <UserRowActions user={user} handlers={handlers} />,
    },
  ]
}

/// 行内操作。三个都可能因为角色、或者「这一行就是你自己」而不可用：一律
/// **禁用 + 一句原因**，不做「点了才告诉你不行」。原因挂在外面那层 `<span>` 的 `title` 上，
/// 因为禁用的按钮自己不派发悬停事件。
function UserRowActions({ user, handlers }: { user: User; handlers: UserColumnHandlers }) {
  const isCurrentUser = handlers.currentUserID === user.id
  const canManage = handlers.manageDisabledReason === undefined

  let statusReason = handlers.manageDisabledReason
  if (statusReason === undefined && isCurrentUser && user.enabled) {
    statusReason = '不能停用自己'
  }
  let resetReason = handlers.manageDisabledReason
  if (resetReason === undefined && isCurrentUser) {
    resetReason = '请从顶栏修改自己的口令'
  }

  return (
    <span className="users-table__actions">
      <span title={statusReason}>
        <Button
          kind="ghost"
          size="sm"
          disabled={statusReason !== undefined || handlers.statusPending}
          onClick={() => handlers.onToggleStatus(user)}
        >
          {user.enabled ? '停用' : '启用'}
        </Button>
      </span>
      <span title={handlers.manageDisabledReason}>
        <Button kind="ghost" size="sm" disabled={!canManage} onClick={() => handlers.onChangeRole(user)}>
          变更角色
        </Button>
      </span>
      <span title={resetReason}>
        <Button
          kind="ghost"
          size="sm"
          disabled={resetReason !== undefined || handlers.resetPending}
          onClick={() => handlers.onResetPassword(user)}
        >
          重置口令
        </Button>
      </span>
    </span>
  )
}

/// 创建用户表单的校验规则。与生成的请求体类型对齐靠两处，漂了就编译不过：
/// `satisfies z.ZodType<UserCreateInput>` 要求 schema 的出参就是请求体，
/// `userCreateBody` 再把出参真的当请求体用。schema 里不写 `transform` / `default`。
const userCreateSchema = z.object({
  username: z.string().refine((value) => value.trim() !== '', '请输入用户名'),
  role: z.enum(roles),
}) satisfies z.ZodType<UserCreateInput>

type UserCreateValues = z.infer<typeof userCreateSchema>

/// 服务端字段错误只接受这两个 —— 两个都有渲染出来的控件可以聚焦。清单之外的字段名
/// 落回整表单的错误条；`setError` 一个表单里没有的名字会挂出一条永远显示不出来、
/// 也永远清不掉的错误。
const userCreateFields = ['username', 'role'] as const satisfies readonly FieldPath<UserCreateValues>[]

function userCreateBody(values: UserCreateValues): UserCreateInput {
  return { username: values.username.trim(), role: values.role }
}

const emptyUserCreateValues: UserCreateValues = { username: '', role: 'READONLY' }

function CreateUserModal({ open, onClose, onCreated }: {
  open: boolean
  onClose: () => void
  onCreated: (created: UserCreated) => void
}) {
  const createUser = $api.useMutation('post', '/api/v1/users')
  const { formState, handleSubmit, register, reset, setError, watch } = useForm<UserCreateValues>({
    resolver: zodResolver(userCreateSchema),
    defaultValues: emptyUserCreateValues,
  })
  const [failure, setFailure] = useState('')

  function close() {
    setFailure('')
    reset(emptyUserCreateValues)
    onClose()
  }

  const submit = handleSubmit((values) => {
    setFailure('')
    createUser.mutate({ body: userCreateBody(values) }, {
      onSuccess: (created) => {
        reset(emptyUserCreateValues)
        onCreated(created)
      },
      onError: (error) => {
        // 字段级错误落到对应控件并聚焦第一个；一条都落不下时才退回整表单的错误条。
        if (applyApiFieldErrors<UserCreateValues>(error, userCreateFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '创建用户失败，请重试'))
        }
      },
    })
  })

  return (
    <Modal
      open={open}
      modalHeading="创建用户"
      primaryButtonText="创建"
      secondaryButtonText="取消"
      primaryButtonDisabled={createUser.isPending}
      onRequestSubmit={() => void submit()}
      onRequestClose={close}
      onSecondarySubmit={close}
      size="sm"
    >
      {/* Modal 的主按钮渲染在 children 之外，点它到不了这里的 onSubmit，所以提交口是
          `onRequestSubmit`；`<form>` 仍然留着，让回车提交与原生表单语义走同一个
          handleSubmit。主按钮**不能**是 type="submit"，那会提交两次。 */}
      <form className="users-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
        <NotificationBar tone="info" title="初始口令由服务端生成，创建成功后只显示这一次。" />
        <FormField label="用户名" required errorText={formState.errors.username?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            autoComplete="off"
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('username')}
          />}
        </FormField>
        <RoleField
          role={watch('role')}
          errorText={formState.errors.role?.message}
          registration={register('role')}
        />
      </form>
    </Modal>
  )
}

const userRoleSchema = z.object({ role: z.enum(roles) }) satisfies z.ZodType<UserRoleInput>

type UserRoleValues = z.infer<typeof userRoleSchema>

const userRoleFields = ['role'] as const satisfies readonly FieldPath<UserRoleValues>[]

function userRoleBody(values: UserRoleValues): UserRoleInput {
  return { role: values.role }
}

function RoleChangeModal({ user, onClose, onSaved }: {
  user: User
  onClose: () => void
  onSaved: () => void
}) {
  const updateRole = $api.useMutation('put', '/api/v1/users/{id}/role')
  const { formState, handleSubmit, register, setError, watch } = useForm<UserRoleValues>({
    resolver: zodResolver(userRoleSchema),
    defaultValues: { role: user.role },
  })
  const [failure, setFailure] = useState('')

  const submit = handleSubmit((values) => {
    setFailure('')
    updateRole.mutate({ params: { path: { id: user.id } }, body: userRoleBody(values) }, {
      onSuccess: onSaved,
      onError: (error) => {
        if (applyApiFieldErrors<UserRoleValues>(error, userRoleFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '变更角色失败，请重试'))
        }
      },
    })
  })

  return (
    <Modal
      open
      modalHeading={`变更 ${user.username} 的角色`}
      primaryButtonText="保存"
      secondaryButtonText="取消"
      primaryButtonDisabled={updateRole.isPending}
      onRequestSubmit={() => void submit()}
      onRequestClose={onClose}
      onSecondarySubmit={onClose}
      size="sm"
    >
      <form className="users-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
        <RoleField
          role={watch('role')}
          errorText={formState.errors.role?.message}
          registration={register('role')}
        />
      </form>
    </Modal>
  )
}

/// 角色选择。两个表单共用一份：「选了这个角色他能做什么」两处都得说，而且必须是同一句。
/// 说明走 `FormField` 的 `helperText`，跟着当前选中的角色变 —— 这是字段说明的既有出口，
/// 不是为这一页新造的浮层。
function RoleField({ role, errorText, registration }: {
  role: Role
  errorText: string | undefined
  registration: UseFormRegisterReturn<'role'>
}) {
  return (
    <FormField label="角色" required helperText={roleCapability(role)} errorText={errorText}>
      {(field) => (
        <Select
          id={field.id}
          labelText=""
          noLabel
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...registration}
        >
          {roles.map((value) => <SelectItem key={value} value={value} text={roleLabel(value)} />)}
        </Select>
      )}
    </FormField>
  )
}

/// 一次性口令对话框。
///
/// **口令只从服务端回来这一次，关掉就再也取不回来**（要再拿只能再重置一次，那是另一次
/// 写操作），所以这里的每一处都为「看清楚并复制走」服务：一条说明它只显示一次的提示条、
/// 一个只读输入框（可以全选、可以看全）、一个复制按钮。
///
/// 组件自己不留任何口令状态：显示什么完全由传进来的 `issued` 决定，`null` 就是没有口令
/// 可显示。关闭之后调用方把它置空，DOM 里就再也没有那串字符 —— 这一条有单元测试盯着。
export function OneTimePasswordModal({ issued, onClose }: { issued: IssuedPassword | null; onClose: () => void }) {
  function copyPassword() {
    if (issued) {
      void navigator.clipboard.writeText(issued.password)
    }
  }

  return (
    <Modal
      open={issued !== null}
      modalHeading={issued?.title ?? ''}
      primaryButtonText="关闭"
      onRequestSubmit={onClose}
      onRequestClose={onClose}
      size="sm"
    >
      <div className="users-password">
        <NotificationBar tone="warning" title="口令仅显示一次，关闭后不再显示" />
        <FormField label="一次性口令">
          {(field) => (
            <div className="users-password__row">
              <TextInput
                id={field.id}
                className="users-password__value"
                labelText=""
                hideLabel
                readOnly
                value={issued?.password ?? ''}
                aria-describedby={field.describedBy}
                // 只读输入框仍然是受控的：React 要一个 onChange，值永远来自 `issued`。
                onChange={() => undefined}
              />
              <CopyButton iconDescription="复制口令" feedback="已复制" onClick={copyPassword} />
            </div>
          )}
        </FormField>
      </div>
    </Modal>
  )
}

function roleLabel(role: Role): string {
  switch (role) {
    case 'READONLY': return '只读运维'
    case 'ALERT_ADMIN': return '告警管理员'
    case 'PLATFORM_ADMIN': return '平台管理员'
    default: return assertNever(role)
  }
}

/// 每个角色能写什么。取自 `internal/securitymodel/tables.go` 的写能力表，不是这里现编的：
/// 只读只改得动自己的口令，告警管理员多出告警处置与告警设置，平台管理员多出实例接入与用户管理。
function roleCapability(role: Role): string {
  switch (role) {
    case 'READONLY': return '可查看全部监控数据，只能修改自己的口令'
    case 'ALERT_ADMIN': return '在只读之上，可处置告警、编辑告警规则与告警设置'
    case 'PLATFORM_ADMIN': return '在告警管理员之上，可管理实例接入与平台用户'
    default: return assertNever(role)
  }
}

function assertNever(value: never): never {
  throw new Error(`unhandled role: ${value}`)
}
