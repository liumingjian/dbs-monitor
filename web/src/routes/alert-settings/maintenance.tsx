import { DeleteOutlined, EditOutlined, PlusOutlined, StopOutlined } from '@ant-design/icons'
import { createRoute } from '@tanstack/react-router'
import {
  Alert,
  Button,
  DatePicker,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import { useCallback, useEffect, useRef, useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'
import { AlertSettingsHeader } from './header'

type MaintenanceWindow = components['schemas']['MaintenanceWindow']
type MaintenanceWindowInput = components['schemas']['MaintenanceWindowInput']
type MaintenanceStatus = components['schemas']['MaintenanceWindowStatus']
type Instance = components['schemas']['Instance']
type Feedback = { type: 'success' | 'error'; text: string }
type MaintenanceForm = {
  instance_ids: string[]
  time_range: [dayjs.Dayjs, dayjs.Dayjs]
  reason: string
}
type MaintenanceSearch = { instance_id?: string }
type MaintenanceSettingsPageProps = {
  initialInstanceId?: string
  openInitially?: boolean
}
type MaintenanceTableProps = {
  windows: MaintenanceWindow[]
  instances: Instance[]
  canManage: boolean
  onEdit: (maintenanceWindow: MaintenanceWindow) => void
  onEnd: (maintenanceWindow: MaintenanceWindow) => void
  onDelete: (maintenanceWindow: MaintenanceWindow) => void
}

const maintenanceTabStatuses: readonly MaintenanceStatus[] = ['ACTIVE', 'SCHEDULED', 'ENDED']

export const maintenanceSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/maintenance-windows',
  component: MaintenanceSettingsPage,
})

export const maintenanceNewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/maintenance-windows/new',
  validateSearch: parseMaintenanceSearch,
  component: MaintenanceShortcutPage,
})

function MaintenanceShortcutPage() {
  const search = maintenanceNewRoute.useSearch()
  return <MaintenanceSettingsPage initialInstanceId={search.instance_id} openInitially />
}

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

function MaintenanceSettingsPage({ initialInstanceId, openInitially = false }: MaintenanceSettingsPageProps) {
  const windowsQuery = $api.useQuery('get', '/api/v1/maintenance-windows')
  const instancesQuery = $api.useQuery('get', '/api/v1/instances')
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const createMutation = $api.useMutation('post', '/api/v1/maintenance-windows')
  const updateMutation = $api.useMutation('put', '/api/v1/maintenance-windows/{id}')
  const endMutation = $api.useMutation('post', '/api/v1/maintenance-windows/{id}/end')
  const deleteMutation = $api.useMutation('delete', '/api/v1/maintenance-windows/{id}')
  const [form] = Form.useForm<MaintenanceForm>()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<MaintenanceWindow | null>(null)
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const openedFromShortcut = useRef(false)
  const role = currentUserQuery.data?.role
  const canManage = role === 'ALERT_ADMIN' || role === 'PLATFORM_ADMIN'

  const openEditor = useCallback((maintenanceWindow?: MaintenanceWindow) => {
    setEditing(maintenanceWindow ?? null)
    form.resetFields()
    form.setFieldsValue(maintenanceWindow ? {
      instance_ids: maintenanceWindow.instance_ids,
      time_range: [dayjs(maintenanceWindow.starts_at), dayjs(maintenanceWindow.ends_at)],
      reason: maintenanceWindow.reason,
    } : {
      instance_ids: initialInstanceId ? [initialInstanceId] : [],
    })
    setOpen(true)
  }, [form, initialInstanceId])

  useEffect(() => {
    if (openInitially && !openedFromShortcut.current) {
      openedFromShortcut.current = true
      openEditor()
    }
  }, [openEditor, openInitially])

  function save(values: MaintenanceForm) {
    const body: MaintenanceWindowInput = {
      instance_ids: values.instance_ids,
      starts_at: values.time_range[0].toISOString(),
      ends_at: values.time_range[1].toISOString(),
      reason: values.reason,
    }
    const options = {
      onSuccess: () => {
        setOpen(false)
        setFeedback({ type: 'success', text: editing ? '维护窗口已更新' : '维护窗口已创建' })
        void windowsQuery.refetch()
      },
      onError: (error: unknown) => setFeedback({ type: 'error', text: apiErrorMessage(error, '保存维护窗口失败') }),
    }
    if (editing) {
      updateMutation.mutate({ params: { path: { id: editing.id } }, body }, options)
      return
    }
    createMutation.mutate({ body }, options)
  }

  function endWindow(maintenanceWindow: MaintenanceWindow) {
    endMutation.mutate({ params: { path: { id: maintenanceWindow.id } } }, {
      onSuccess: () => void windowsQuery.refetch(),
      onError: (error) => setFeedback({ type: 'error', text: apiErrorMessage(error, '提前结束维护窗口失败') }),
    })
  }

  function removeWindow(maintenanceWindow: MaintenanceWindow) {
    deleteMutation.mutate({ params: { path: { id: maintenanceWindow.id } } }, {
      onSuccess: () => void windowsQuery.refetch(),
      onError: (error) => setFeedback({ type: 'error', text: apiErrorMessage(error, '删除维护窗口失败') }),
    })
  }

  const windows = windowsQuery.data ?? []
  const grouped = groupMaintenanceWindows(windows)
  const instances = instancesQuery.data ?? []
  return (
    <Space orientation="vertical" size="large" style={{ width: '100%' }}>
      <AlertSettingsHeader active="maintenance" />
      {!canManage && <Alert type="info" showIcon title="只读模式" description="需要告警管理员角色才能管理维护窗口" />}
      {feedback && <Alert type={feedback.type} title={feedback.text} closable onClose={() => setFeedback(null)} />}
      <Space className="settings-section-heading" wrap>
        <Typography.Title level={4} style={{ margin: 0 }}>维护窗口</Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} disabled={!canManage} onClick={() => openEditor()}>新建维护窗口</Button>
      </Space>
      <Tabs items={maintenanceTabStatuses.map((status) => ({
        key: status,
        label: `${statusLabel(status)} ${grouped[status].length}`,
        children: <MaintenanceTable
          windows={grouped[status]}
          instances={instances}
          canManage={canManage}
          onEdit={openEditor}
          onEnd={endWindow}
          onDelete={removeWindow}
        />,
      }))} />
      <Modal
        title={editing ? '编辑维护窗口' : '新建维护窗口'}
        open={open}
        footer={null}
        destroyOnHidden
        onCancel={() => setOpen(false)}
      >
        <Form<MaintenanceForm> form={form} layout="vertical" onFinish={save}>
          <Form.Item name="instance_ids" label="实例" rules={[{ required: true, message: '请至少选择一个实例' }]}>
            <Select mode="multiple" options={instances.map((instance) => ({ value: instance.id, label: instance.name }))} />
          </Form.Item>
          <Form.Item name="time_range" label="起止时间" rules={[{ required: true, message: '请选择起止时间' }]}>
            <DatePicker.RangePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="reason" label="原因" rules={[{ required: true, whitespace: true }]}>
            <Input.TextArea rows={3} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={createMutation.isPending || updateMutation.isPending}>
            保存
          </Button>
        </Form>
      </Modal>
    </Space>
  )
}

function MaintenanceTable({ windows, instances, canManage, onEdit, onEnd, onDelete }: MaintenanceTableProps) {
  const instanceNames = new Map(instances.map((instance) => [instance.id, instance.name]))
  const columns: TableColumnsType<MaintenanceWindow> = [
    { title: '原因', dataIndex: 'reason' },
    {
      title: '实例',
      render: (_, maintenanceWindow) => maintenanceWindow.instance_ids
        .map((id) => instanceNames.get(id) ?? id)
        .join('、'),
    },
    {
      title: '状态',
      width: 90,
      render: (_, maintenanceWindow) => (
        <Tag color={statusColor(maintenanceWindow.status)}>{statusLabel(maintenanceWindow.status)}</Tag>
      ),
    },
    { title: '开始时间', width: 180, render: (_, maintenanceWindow) => formatTime(maintenanceWindow.starts_at) },
    { title: '结束时间', width: 180, render: (_, maintenanceWindow) => formatTime(maintenanceWindow.ends_at) },
    { title: '创建人', width: 130, render: (_, maintenanceWindow) => maintenanceWindow.created_by.slice(0, 8) },
    {
      title: '操作',
      width: 150,
      fixed: 'right',
      render: (_, maintenanceWindow) => {
        const editDisabled = !canManage || maintenanceWindow.status === 'ENDED'
        const endDisabled = !canManage || maintenanceWindow.status !== 'ACTIVE'
        return (
          <Space>
            <Tooltip title={maintenanceWindow.status === 'ENDED' ? '已结束窗口不可编辑' : '编辑维护窗口'}>
              <span>
                <Button
                  aria-label={`编辑 ${maintenanceWindow.reason}`}
                  icon={<EditOutlined />}
                  disabled={editDisabled}
                  onClick={() => onEdit(maintenanceWindow)}
                />
              </span>
            </Tooltip>
            <Popconfirm
              title="提前结束此维护窗口？"
              disabled={endDisabled}
              onConfirm={() => onEnd(maintenanceWindow)}
            >
              <Tooltip title="提前结束">
                <span>
                  <Button
                    aria-label={`提前结束 ${maintenanceWindow.reason}`}
                    icon={<StopOutlined />}
                    disabled={endDisabled}
                  />
                </span>
              </Tooltip>
            </Popconfirm>
            <Popconfirm title="删除此维护窗口？" disabled={!canManage} onConfirm={() => onDelete(maintenanceWindow)}>
              <Tooltip title="删除维护窗口">
                <Button
                  aria-label={`删除 ${maintenanceWindow.reason}`}
                  danger
                  icon={<DeleteOutlined />}
                  disabled={!canManage}
                />
              </Tooltip>
            </Popconfirm>
          </Space>
        )
      },
    },
  ]

  return (
    <Table<MaintenanceWindow>
      rowKey="id"
      dataSource={windows}
      pagination={false}
      scroll={{ x: 960 }}
      columns={columns}
    />
  )
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

function statusColor(status: MaintenanceStatus): string {
  switch (status) {
    case 'ACTIVE':
      return 'processing'
    case 'SCHEDULED':
      return 'gold'
    case 'ENDED':
      return 'default'
    default:
      return assertNever(status)
  }
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function assertNever(value: never): never {
  throw new Error(`unexpected maintenance window status: ${value}`)
}
