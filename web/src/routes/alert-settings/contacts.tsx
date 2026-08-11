import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { createRoute } from '@tanstack/react-router'
import { Alert, Button, Form, Input, Modal, Popconfirm, Select, Space, Table, Tooltip, Typography } from 'antd'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'
import { AlertSettingsHeader } from './header'

type Contact = components['schemas']['NotificationContact']
type ContactInput = components['schemas']['NotificationContactInput']
type ContactGroup = components['schemas']['NotificationContactGroup']
type ContactGroupInput = components['schemas']['NotificationContactGroupInput']
type Feedback = { type: 'success' | 'error'; text: string }

type ContactsTableProps = {
  contacts: Contact[]
  loading: boolean
  canManage: boolean
  onEdit: (contact: Contact) => void
  onDelete: (contact: Contact) => void
}

type ContactGroupsTableProps = {
  groups: ContactGroup[]
  contactNames: Map<string, string>
  loading: boolean
  canManage: boolean
  onEdit: (group: ContactGroup) => void
  onDelete: (group: ContactGroup) => void
}

export const contactSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/contacts',
  component: ContactSettingsPage,
})

function ContactSettingsPage() {
  const contactsQuery = $api.useQuery('get', '/api/v1/notification-contacts')
  const groupsQuery = $api.useQuery('get', '/api/v1/notification-contact-groups')
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const createContactMutation = $api.useMutation('post', '/api/v1/notification-contacts')
  const updateContactMutation = $api.useMutation('put', '/api/v1/notification-contacts/{id}')
  const deleteContactMutation = $api.useMutation('delete', '/api/v1/notification-contacts/{id}')
  const createGroupMutation = $api.useMutation('post', '/api/v1/notification-contact-groups')
  const updateGroupMutation = $api.useMutation('put', '/api/v1/notification-contact-groups/{id}')
  const deleteGroupMutation = $api.useMutation('delete', '/api/v1/notification-contact-groups/{id}')
  const [contactForm] = Form.useForm<ContactInput>()
  const [groupForm] = Form.useForm<ContactGroupInput>()
  const [contactOpen, setContactOpen] = useState(false)
  const [groupOpen, setGroupOpen] = useState(false)
  const [editingContact, setEditingContact] = useState<Contact | null>(null)
  const [editingGroup, setEditingGroup] = useState<ContactGroup | null>(null)
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const role = currentUserQuery.data?.role
  const canManage = role === 'ALERT_ADMIN' || role === 'PLATFORM_ADMIN'

  function openContact(contact?: Contact) {
    setEditingContact(contact ?? null)
    contactForm.resetFields()
    if (contact) contactForm.setFieldsValue({ name: contact.name, email: contact.email, external_id: contact.external_id })
    setContactOpen(true)
  }

  function saveContact(values: ContactInput) {
    const options = {
      onSuccess: () => {
        setContactOpen(false)
        setFeedback({ type: 'success' as const, text: editingContact ? '联系人已更新' : '联系人已创建' })
        void contactsQuery.refetch()
      },
      onError: (error: unknown) => setFeedback({ type: 'error' as const, text: apiErrorMessage(error, '保存联系人失败') }),
    }
    if (editingContact) {
      updateContactMutation.mutate({ params: { path: { id: editingContact.id } }, body: values }, options)
    } else {
      createContactMutation.mutate({ body: values }, options)
    }
  }

  function removeContact(contact: Contact) {
    deleteContactMutation.mutate(
      { params: { path: { id: contact.id } } },
      {
        onSuccess: () => void contactsQuery.refetch(),
        onError: (error) => setFeedback({ type: 'error', text: apiErrorMessage(error, '删除联系人失败') }),
      },
    )
  }

  function openGroup(group?: ContactGroup) {
    setEditingGroup(group ?? null)
    groupForm.resetFields()
    groupForm.setFieldsValue(group ? { name: group.name, contact_ids: group.contact_ids } : { contact_ids: [] })
    setGroupOpen(true)
  }

  function saveGroup(values: ContactGroupInput) {
    const options = {
      onSuccess: () => {
        setGroupOpen(false)
        setFeedback({ type: 'success' as const, text: editingGroup ? '联系人组已更新' : '联系人组已创建' })
        void groupsQuery.refetch()
      },
      onError: (error: unknown) => setFeedback({ type: 'error' as const, text: apiErrorMessage(error, '保存联系人组失败') }),
    }
    if (editingGroup) {
      updateGroupMutation.mutate({ params: { path: { id: editingGroup.id } }, body: values }, options)
    } else {
      createGroupMutation.mutate({ body: values }, options)
    }
  }

  function removeGroup(group: ContactGroup) {
    deleteGroupMutation.mutate(
      { params: { path: { id: group.id } } },
      {
        onSuccess: () => void groupsQuery.refetch(),
        onError: (error) => setFeedback({ type: 'error', text: apiErrorMessage(error, '删除联系人组失败') }),
      },
    )
  }

  const contactNames = new Map((contactsQuery.data ?? []).map((contact) => [contact.id, contact.name]))
  return (
    <Space orientation="vertical" size="large" style={{ width: '100%' }}>
      <AlertSettingsHeader active="contacts" />
      {!canManage && <Alert type="info" showIcon title="只读模式" description="需要告警管理员角色才能修改联系人和联系人组" />}
      {feedback && <Alert type={feedback.type} title={feedback.text} closable onClose={() => setFeedback(null)} />}
      <section className="settings-section">
        <Space className="settings-section-heading" wrap>
          <Typography.Title level={4} style={{ margin: 0 }}>联系人</Typography.Title>
          <Button type="primary" icon={<PlusOutlined />} disabled={!canManage} onClick={() => openContact()}>新建联系人</Button>
        </Space>
        <ContactsTable
          contacts={contactsQuery.data ?? []}
          loading={contactsQuery.isPending}
          canManage={canManage}
          onEdit={openContact}
          onDelete={removeContact}
        />
      </section>
      <section className="settings-section">
        <Space className="settings-section-heading" wrap>
          <Typography.Title level={4} style={{ margin: 0 }}>联系人组</Typography.Title>
          <Button type="primary" icon={<PlusOutlined />} disabled={!canManage} onClick={() => openGroup()}>新建联系人组</Button>
        </Space>
        <ContactGroupsTable
          groups={groupsQuery.data ?? []}
          contactNames={contactNames}
          loading={groupsQuery.isPending}
          canManage={canManage}
          onEdit={openGroup}
          onDelete={removeGroup}
        />
      </section>
      <Modal title={editingContact ? '编辑联系人' : '新建联系人'} open={contactOpen} footer={null} destroyOnHidden onCancel={() => setContactOpen(false)}>
        <Form<ContactInput> form={contactForm} layout="vertical" onFinish={saveContact}>
          <Form.Item name="name" label="姓名" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          <Form.Item name="email" label="邮箱" rules={[{ required: true, type: 'email' }]}><Input /></Form.Item>
          <Form.Item name="external_id" label="外部 ID"><Input /></Form.Item>
          <Button type="primary" htmlType="submit" loading={createContactMutation.isPending || updateContactMutation.isPending}>保存</Button>
        </Form>
      </Modal>
      <Modal title={editingGroup ? '编辑联系人组' : '新建联系人组'} open={groupOpen} footer={null} destroyOnHidden onCancel={() => setGroupOpen(false)}>
        <Form<ContactGroupInput> form={groupForm} layout="vertical" onFinish={saveGroup}>
          <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          <Form.Item name="contact_ids" label="成员"><Select mode="multiple" options={(contactsQuery.data ?? []).map((contact) => ({ value: contact.id, label: `${contact.name} · ${contact.email}` }))} /></Form.Item>
          <Button type="primary" htmlType="submit" loading={createGroupMutation.isPending || updateGroupMutation.isPending}>保存</Button>
        </Form>
      </Modal>
    </Space>
  )
}

function ContactsTable({ contacts, loading, canManage, onEdit, onDelete }: ContactsTableProps) {
  return (
    <Table<Contact>
      rowKey="id"
      loading={loading}
      dataSource={contacts}
      pagination={false}
      columns={[
        { title: '姓名', dataIndex: 'name' },
        { title: '邮箱', dataIndex: 'email' },
        { title: '外部 ID', dataIndex: 'external_id', render: (value?: string) => value ?? '-' },
        {
          title: '操作',
          width: 120,
          render: (_, contact) => (
            <Space>
              <Tooltip title="编辑联系人">
                <Button aria-label={`编辑 ${contact.name}`} icon={<EditOutlined />} disabled={!canManage} onClick={() => onEdit(contact)} />
              </Tooltip>
              <Popconfirm title="删除此联系人？" disabled={!canManage} onConfirm={() => onDelete(contact)}>
                <Tooltip title="删除联系人">
                  <Button aria-label={`删除 ${contact.name}`} danger icon={<DeleteOutlined />} disabled={!canManage} />
                </Tooltip>
              </Popconfirm>
            </Space>
          ),
        },
      ]}
    />
  )
}

function ContactGroupsTable({ groups, contactNames, loading, canManage, onEdit, onDelete }: ContactGroupsTableProps) {
  return (
    <Table<ContactGroup>
      rowKey="id"
      loading={loading}
      dataSource={groups}
      pagination={false}
      columns={[
        { title: '名称', dataIndex: 'name' },
        {
          title: '成员',
          render: (_, group) => group.contact_ids.map((id) => contactNames.get(id) ?? id).join('、') || '-',
        },
        {
          title: '操作',
          width: 120,
          render: (_, group) => (
            <Space>
              <Tooltip title="编辑联系人组">
                <Button aria-label={`编辑 ${group.name}`} icon={<EditOutlined />} disabled={!canManage} onClick={() => onEdit(group)} />
              </Tooltip>
              <Popconfirm title="删除此联系人组？" disabled={!canManage} onConfirm={() => onDelete(group)}>
                <Tooltip title="删除联系人组">
                  <Button aria-label={`删除 ${group.name}`} danger icon={<DeleteOutlined />} disabled={!canManage} />
                </Tooltip>
              </Popconfirm>
            </Space>
          ),
        },
      ]}
    />
  )
}
