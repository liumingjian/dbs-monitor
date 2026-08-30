import {
  Button,
  DatePicker,
  DatePickerInput,
  Modal,
  MultiSelect,
  Tab,
  TabList,
  TabPanel,
  TabPanels,
  Tabs,
  TextArea,
  TimePicker,
} from '@carbon/react'
import { useId, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import type { FieldPath } from 'react-hook-form'
import { z } from 'zod'
import { $api } from '../../api/client'
import { apiErrorMessage, applyApiFieldErrors } from '../../api/errors'
import type { components } from '../../api/schema'
import { zodResolver } from '../../forms/zodResolver'
import { DataGrid } from '../../primitives/DataGrid'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { StatusBadge } from '../../primitives/StatusBadge'
import type { StatusTone } from '../../primitives/StatusBadge'
import { TruncatedText } from '../../primitives/TruncatedText'
import { ConfirmedAction, InlineAction } from './ConfirmedAction'
import {
  composeLocalDateTime,
  datePattern,
  formatDateTimeRange,
  localTimeZoneLabel,
  splitLocalDateTime,
  timePattern,
} from './dateTimeRange'
import { readOnlyReason } from './shared'
import type { Feedback } from './shared'

type MaintenanceWindow = components['schemas']['MaintenanceWindow']
type MaintenanceWindowInput = components['schemas']['MaintenanceWindowInput']
type MaintenanceStatus = components['schemas']['MaintenanceWindowStatus']
type InstanceOption = { id: string; label: string }
type MaintenanceSearch = { instance_id?: string }

const maintenanceTabStatuses: readonly MaintenanceStatus[] = ['ACTIVE', 'SCHEDULED', 'ENDED']

/// 维护窗口快捷入口（实例总览的「进入维护」）带来的实例。合并前它是 `/…/new` 这个地址的
/// search 校验器，合并后旧地址仍然存在、只是改为重定向，解析规则原样保留。
export function parseMaintenanceSearch(search: Record<string, unknown>): MaintenanceSearch {
  if (typeof search.instance_id === 'string' && search.instance_id !== '') {
    return { instance_id: search.instance_id }
  }
  return {}
}

export function groupMaintenanceWindows(windows: MaintenanceWindow[]): Record<MaintenanceStatus, MaintenanceWindow[]> {
  const grouped: Record<MaintenanceStatus, MaintenanceWindow[]> = { ACTIVE: [], SCHEDULED: [], ENDED: [] }
  for (const maintenanceWindow of windows) {
    switch (maintenanceWindow.status) {
      case 'ACTIVE':
      case 'SCHEDULED':
      case 'ENDED':
        grouped[maintenanceWindow.status].push(maintenanceWindow)
        break
      default:
        assertNever(maintenanceWindow.status)
    }
  }
  return grouped
}

function statusLabel(status: MaintenanceStatus): string {
  switch (status) {
    case 'ACTIVE':
      return '生效中'
    case 'SCHEDULED':
      return '未开始'
    case 'ENDED':
      return '已结束'
    default:
      return assertNever(status)
  }
}

function statusTone(status: MaintenanceStatus): StatusTone {
  switch (status) {
    case 'ACTIVE':
      return 'normal'
    case 'SCHEDULED':
      return 'warning'
    case 'ENDED':
      return 'unknown'
    default:
      return assertNever(status)
  }
}

function assertNever(value: never): never {
  throw new Error(`unexpected maintenance window status: ${value}`)
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function AddIcon() {
  return <Icon name="add" />
}

// ---------------------------------------------------------------------------
// 表单
// ---------------------------------------------------------------------------

/// 日期与时刻分成四个字段：组件库没有日期时间范围选择器，日期范围是一个控件、
/// 时刻是另外两个，合成放在 `maintenanceWindowBody` 里（见 `dateTimeRange.ts`）。
const maintenanceSchema = z
  .object({
    instance_ids: z.array(z.string()).min(1, '请至少选择一个实例'),
    start_date: z.string().regex(datePattern, '请选择开始日期'),
    start_time: z.string().regex(timePattern, '请输入开始时刻，24 小时制 HH:mm'),
    end_date: z.string().regex(datePattern, '请选择结束日期'),
    end_time: z.string().regex(timePattern, '请输入结束时刻，24 小时制 HH:mm'),
    reason: z.string().refine((value) => value.trim() !== '', '请输入维护原因'),
  })
  .refine(
    (values) => {
      const start = composeLocalDateTime({ date: values.start_date, time: values.start_time })
      const end = composeLocalDateTime({ date: values.end_date, time: values.end_time })
      // 任一端合不成时这条规则不表态：缺什么由各字段自己的规则去报。
      return start === undefined || end === undefined || Date.parse(end) > Date.parse(start)
    },
    { path: ['end_time'], error: '结束时间必须晚于开始时间' },
  )

type MaintenanceValues = z.infer<typeof maintenanceSchema>

const maintenanceFields = [
  'instance_ids',
  'start_date',
  'start_time',
  'end_date',
  'end_time',
  'reason',
] as const satisfies readonly FieldPath<MaintenanceValues>[]

/// 表单值 → 请求体。合成失败在这里是不可能的：schema 已经保证两端都合格，
/// 但类型上仍然是 `string | undefined`，所以显式抛而不是 `?? ''` 兜底成一个假时刻。
export function maintenanceWindowBody(values: MaintenanceValues): MaintenanceWindowInput {
  const startsAt = composeLocalDateTime({ date: values.start_date, time: values.start_time })
  const endsAt = composeLocalDateTime({ date: values.end_date, time: values.end_time })
  if (startsAt === undefined || endsAt === undefined) {
    throw new Error('maintenance window submitted with an incomplete date and time')
  }
  return {
    instance_ids: values.instance_ids,
    starts_at: startsAt,
    ends_at: endsAt,
    reason: values.reason.trim(),
  }
}

export function maintenanceFormValues(maintenanceWindow: MaintenanceWindow | null, initialInstanceID?: string): MaintenanceValues {
  if (maintenanceWindow === null) {
    return {
      instance_ids: initialInstanceID === undefined ? [] : [initialInstanceID],
      start_date: '',
      start_time: '',
      end_date: '',
      end_time: '',
      reason: '',
    }
  }
  const start = splitLocalDateTime(maintenanceWindow.starts_at)
  const end = splitLocalDateTime(maintenanceWindow.ends_at)
  return {
    instance_ids: maintenanceWindow.instance_ids,
    start_date: start.date,
    start_time: start.time,
    end_date: end.date,
    end_time: end.time,
    reason: maintenanceWindow.reason,
  }
}

// ---------------------------------------------------------------------------
// 标签页
// ---------------------------------------------------------------------------

/// 「维护窗口」标签。
///
/// 内层还有一条按状态分的页签条（生效中 / 未开始 / 已结束）。它**不是**导航 ——
/// 三档是同一张表的筛选，不改变地址，所以这一层用受控 `Tabs`，与外层那条
/// 「页签即地址」的导航页签条不是一回事。
export function MaintenancePanel({ canManage, initialInstanceID, openInitially, onEditorOpened }: {
  canManage: boolean
  initialInstanceID?: string
  openInitially: boolean
  onEditorOpened: () => void
}) {
  const windowsQuery = $api.useQuery('get', '/api/v1/maintenance-windows')
  const instancesQuery = $api.useQuery('get', '/api/v1/instances')
  const endMutation = $api.useMutation('post', '/api/v1/maintenance-windows/{id}/end')
  const deleteMutation = $api.useMutation('delete', '/api/v1/maintenance-windows/{id}')
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const [editor, setEditor] = useState<{ maintenanceWindow: MaintenanceWindow | null } | null>(
    openInitially ? { maintenanceWindow: null } : null,
  )
  const [statusIndex, setStatusIndex] = useState(0)

  const windows = windowsQuery.data ?? []
  const grouped = groupMaintenanceWindows(windows)
  const instances = instancesQuery.data ?? []
  const instanceNames = new Map(instances.map((instance) => [instance.id, instance.name]))
  const instanceOptions: InstanceOption[] = instances.map((instance) => ({ id: instance.id, label: instance.name }))

  function closeEditor() {
    setEditor(null)
    onEditorOpened()
  }

  function endWindow(maintenanceWindow: MaintenanceWindow) {
    setFeedback(null)
    endMutation.mutate({ params: { path: { id: maintenanceWindow.id } } }, {
      onSuccess: () => {
        setFeedback({ tone: 'normal', text: '维护窗口已提前结束' })
        void windowsQuery.refetch()
      },
      onError: (error) => setFeedback({ tone: 'critical', text: apiErrorMessage(error, '提前结束维护窗口失败') }),
    })
  }

  function removeWindow(maintenanceWindow: MaintenanceWindow) {
    setFeedback(null)
    deleteMutation.mutate({ params: { path: { id: maintenanceWindow.id } } }, {
      onSuccess: () => {
        setFeedback({ tone: 'normal', text: '维护窗口已删除' })
        void windowsQuery.refetch()
      },
      onError: (error) => setFeedback({ tone: 'critical', text: apiErrorMessage(error, '删除维护窗口失败') }),
    })
  }

  return (
    <div className="alert-settings-stack">
      {feedback !== null && (
        <NotificationBar tone={feedback.tone} title={feedback.text} onClose={() => setFeedback(null)} />
      )}
      {windowsQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(windowsQuery.error, '维护窗口加载失败')} />
      )}
      <Panel
        flush
        title="维护窗口"
        description="窗口生效期间，覆盖到的实例不再派发通知。"
        actions={<span title={canManage ? undefined : readOnlyReason.maintenance}>
          <Button size="sm" renderIcon={AddIcon} disabled={!canManage} onClick={() => setEditor({ maintenanceWindow: null })}>
            新建维护窗口
          </Button>
        </span>}
      >
        <Tabs selectedIndex={statusIndex} onChange={({ selectedIndex }) => setStatusIndex(selectedIndex)}>
          <TabList aria-label="维护窗口状态" activation="manual" contained>
            {maintenanceTabStatuses.map((status) => (
              <Tab key={status}>{`${statusLabel(status)} ${grouped[status].length}`}</Tab>
            ))}
          </TabList>
          <TabPanels>
            {maintenanceTabStatuses.map((status, index) => (
              <TabPanel key={status}>
                {index === statusIndex && (
                  <MaintenanceTable
                    status={status}
                    windows={grouped[status]}
                    instanceNames={instanceNames}
                    canManage={canManage}
                    loading={windowsQuery.isPending}
                    onEdit={(maintenanceWindow) => setEditor({ maintenanceWindow })}
                    onEnd={endWindow}
                    onDelete={removeWindow}
                  />
                )}
              </TabPanel>
            ))}
          </TabPanels>
        </Tabs>
      </Panel>
      {editor !== null && (
        <MaintenanceModal
          maintenanceWindow={editor.maintenanceWindow}
          initialInstanceID={initialInstanceID}
          instanceOptions={instanceOptions}
          onClose={closeEditor}
          onSaved={(message) => {
            closeEditor()
            setFeedback({ tone: 'normal', text: message })
            void windowsQuery.refetch()
          }}
        />
      )}
    </div>
  )
}

function MaintenanceTable({ status, windows, instanceNames, canManage, loading, onEdit, onEnd, onDelete }: {
  status: MaintenanceStatus
  windows: MaintenanceWindow[]
  instanceNames: Map<string, string>
  canManage: boolean
  loading: boolean
  onEdit: (maintenanceWindow: MaintenanceWindow) => void
  onEnd: (maintenanceWindow: MaintenanceWindow) => void
  onDelete: (maintenanceWindow: MaintenanceWindow) => void
}) {
  return (
    <DataGrid<MaintenanceWindow>
      label={`${statusLabel(status)}的维护窗口`}
      loading={loading}
      rows={windows}
      rowKey={(maintenanceWindow) => maintenanceWindow.id}
      rowTestId="maintenance-window-row"
      columns={[
        { key: 'reason', header: '原因', minWidth: 160, cell: (item) => <TruncatedText className="alert-settings-strong">{item.reason}</TruncatedText> },
        {
          key: 'instances',
          header: '实例',
          minWidth: 180,
          cell: (item) => (
            <TruncatedText>{item.instance_ids.map((id) => instanceNames.get(id) ?? id).join('、')}</TruncatedText>
          ),
        },
        {
          key: 'status',
          header: '状态',
          minWidth: 92,
          grow: 1.5,
          cell: (item) => <StatusBadge tone={statusTone(item.status)}>{statusLabel(item.status)}</StatusBadge>,
        },
        { key: 'starts_at', header: '开始时间', minWidth: 160, numeric: true, grow: 1.2, cell: (item) => <TruncatedText>{formatTime(item.starts_at)}</TruncatedText> },
        { key: 'ends_at', header: '结束时间', minWidth: 160, numeric: true, grow: 1.2, cell: (item) => <TruncatedText>{formatTime(item.ends_at)}</TruncatedText> },
        { key: 'created_by', header: '创建人', minWidth: 110, cell: (item) => <TruncatedText title={item.created_by}>{item.created_by.slice(0, 8)}</TruncatedText> },
        {
          key: 'actions',
          header: '操作',
          minWidth: 128,
          grow: 1.6,
          align: 'end',
          cell: (item) => {
            const ended = item.status === 'ENDED'
            return (
              <span className="alert-settings-row-actions">
                <InlineAction
                  name={`编辑 ${item.reason}`}
                  icon="edit"
                  disabled={!canManage || ended}
                  disabledReason={ended ? '已结束的窗口不可编辑' : readOnlyReason.maintenance}
                  onClick={() => onEdit(item)}
                />
                <ConfirmedAction
                  name={`提前结束 ${item.reason}`}
                  icon="stop"
                  heading="提前结束维护窗口"
                  description={`结束后，这个窗口覆盖的实例立即恢复派发通知，抑制期间的告警不会补发。`}
                  confirmLabel="提前结束"
                  disabled={!canManage || item.status !== 'ACTIVE'}
                  disabledReason={canManage ? '只有生效中的窗口可以提前结束' : readOnlyReason.maintenance}
                  onConfirm={() => onEnd(item)}
                />
                <ConfirmedAction
                  name={`删除 ${item.reason}`}
                  icon="trashCan"
                  destructive
                  heading="删除维护窗口"
                  description={`删除后这段维护记录不再保留，${ended ? '历史告警的维护标记也会失去依据' : '覆盖的实例立即恢复派发通知'}。此操作不可撤销。`}
                  confirmLabel="删除窗口"
                  disabled={!canManage}
                  disabledReason={readOnlyReason.maintenance}
                  onConfirm={() => onDelete(item)}
                />
              </span>
            )
          },
        },
      ]}
      empty={{ title: `没有${statusLabel(status)}的维护窗口` }}
    />
  )
}

function MaintenanceModal({ maintenanceWindow, initialInstanceID, instanceOptions, onClose, onSaved }: {
  maintenanceWindow: MaintenanceWindow | null
  initialInstanceID?: string
  instanceOptions: InstanceOption[]
  onClose: () => void
  onSaved: (message: string) => void
}) {
  const createMutation = $api.useMutation('post', '/api/v1/maintenance-windows')
  const updateMutation = $api.useMutation('put', '/api/v1/maintenance-windows/{id}')
  const [failure, setFailure] = useState('')
  const endDateID = useId()
  const { control, formState, handleSubmit, register, setError, setValue, watch } = useForm<MaintenanceValues>({
    resolver: zodResolver(maintenanceSchema),
    defaultValues: maintenanceFormValues(maintenanceWindow, initialInstanceID),
  })

  const startDate = watch('start_date')
  const startTime = watch('start_time')
  const endDate = watch('end_date')
  const endTime = watch('end_time')
  const composed = formatDateTimeRange({ date: startDate, time: startTime }, { date: endDate, time: endTime })

  const submit = handleSubmit((values) => {
    setFailure('')
    const body = maintenanceWindowBody(values)
    const options = {
      onSuccess: () => onSaved(maintenanceWindow === null ? '维护窗口已创建' : '维护窗口已更新'),
      onError: (error: unknown) => {
        if (applyApiFieldErrors<MaintenanceValues>(error, maintenanceFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '保存维护窗口失败'))
        }
      },
    }
    if (maintenanceWindow !== null) {
      updateMutation.mutate({ params: { path: { id: maintenanceWindow.id } }, body }, options)
      return
    }
    createMutation.mutate({ body }, options)
  })

  // 日历给回来的是 Date[]；两端各自写进对应字段，用户键入的日期文本也会走这里。
  function pickDates(dates: Date[]) {
    setValue('start_date', dates[0] === undefined ? '' : toDateValue(dates[0]), { shouldValidate: formState.isSubmitted })
    setValue('end_date', dates[1] === undefined ? '' : toDateValue(dates[1]), { shouldValidate: formState.isSubmitted })
  }

  return (
    <Modal
      open
      modalHeading={maintenanceWindow === null ? '新建维护窗口' : '编辑维护窗口'}
      primaryButtonText="保存维护窗口"
      secondaryButtonText="取消"
      primaryButtonDisabled={createMutation.isPending || updateMutation.isPending}
      size="md"
      onRequestSubmit={() => void submit()}
      onRequestClose={onClose}
      onSecondarySubmit={onClose}
    >
      <form className="alert-settings-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
        <FormField label="实例" required errorText={formState.errors.instance_ids?.message}>
          {(field) => <Controller
            name="instance_ids"
            control={control}
            render={({ field: value }) => <MultiSelect<InstanceOption>
              id={field.id}
              titleText=""
              hideLabel
              label="选择实例"
              items={instanceOptions}
              itemToString={(item) => item?.label ?? ''}
              selectedItems={instanceOptions.filter((option) => value.value.includes(option.id))}
              invalid={field.invalid}
              aria-describedby={field.describedBy}
              onChange={({ selectedItems }) => value.onChange((selectedItems ?? []).map((item) => item.id))}
            />}
          />}
        </FormField>

        {/* 起止时间：**日期与时刻是四个独立控件**，组件库没有日期时间范围选择器。
            日期用一个 range 型 DatePicker（两个输入框，可直接键入），时刻各一个 TimePicker。
            四个数字读起来容易对错，所以下面回显一句合成后的完整值与时长。 */}
        <fieldset className="alert-settings-datetime">
          <legend className="alert-settings-datetime__legend dbs-caption">起止时间</legend>
          <FormField errorText={formState.errors.start_date?.message ?? formState.errors.end_date?.message}>
            {(field) => <DatePicker
              datePickerType="range"
              dateFormat="Y-m-d"
              value={[startDate, endDate]}
              onChange={pickDates}
            >
              <DatePickerInput
                id={field.id}
                labelText="开始日期"
                placeholder="YYYY-MM-DD"
                size="md"
                invalid={formState.errors.start_date !== undefined}
                aria-describedby={field.describedBy}
              />
              <DatePickerInput
                id={endDateID}
                labelText="结束日期"
                placeholder="YYYY-MM-DD"
                size="md"
                invalid={formState.errors.end_date !== undefined}
              />
            </DatePicker>}
          </FormField>
          <div className="alert-settings-form__row">
            <FormField label="开始时刻" required errorText={formState.errors.start_time?.message}>
              {(field) => <TimePicker
                id={field.id}
                labelText=""
                hideLabel
                placeholder="HH:mm"
                maxLength={5}
                size="md"
                invalid={field.invalid}
                aria-describedby={field.describedBy}
                {...register('start_time')}
              />}
            </FormField>
            <FormField label="结束时刻" required errorText={formState.errors.end_time?.message}>
              {(field) => <TimePicker
                id={field.id}
                labelText=""
                hideLabel
                placeholder="HH:mm"
                maxLength={5}
                size="md"
                invalid={field.invalid}
                aria-describedby={field.describedBy}
                {...register('end_time')}
              />}
            </FormField>
          </div>
          <p className="alert-settings-datetime__echo dbs-caption">
            {composed === '' ? `按本机时区（${localTimeZoneLabel()}）解读，24 小时制。` : `${composed}，时区 ${localTimeZoneLabel()}。`}
          </p>
        </fieldset>

        <FormField label="原因" required errorText={formState.errors.reason?.message}>
          {(field) => <TextArea
            id={field.id}
            labelText=""
            hideLabel
            rows={3}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('reason')}
          />}
        </FormField>
      </form>
    </Modal>
  )
}

function toDateValue(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}
