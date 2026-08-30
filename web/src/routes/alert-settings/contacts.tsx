import { Button, Modal, MultiSelect, TextInput } from '@carbon/react'
import { useState } from 'react'
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
import { TruncatedText } from '../../primitives/TruncatedText'
import { ConfirmedAction, InlineAction } from './ConfirmedAction'
import { emailPattern, readOnlyReason } from './shared'
import type { Feedback } from './shared'

type Contact = components['schemas']['NotificationContact']
type ContactInput = components['schemas']['NotificationContactInput']
type ContactGroup = components['schemas']['NotificationContactGroup']
type ContactGroupInput = components['schemas']['NotificationContactGroupInput']
type ContactOption = { id: string; label: string }

const contactSchema = z.object({
  name: z.string().refine((value) => value.trim() !== '', '请输入姓名'),
  email: z.string().regex(emailPattern, '请输入有效的邮箱'),
  external_id: z.string(),
})

type ContactValues = z.infer<typeof contactSchema>

const contactFields = ['name', 'email', 'external_id'] as const satisfies readonly FieldPath<ContactValues>[]

export function contactBody(values: ContactValues): ContactInput {
  const body: ContactInput = { name: values.name.trim(), email: values.email.trim() }
  // 空串是「没填」，不是「设成空字符串」：归一化放在提交处，schema 里不写 transform。
  if (values.external_id.trim() !== '') body.external_id = values.external_id.trim()
  return body
}

const groupSchema = z.object({
  name: z.string().refine((value) => value.trim() !== '', '请输入名称'),
  contact_ids: z.array(z.string()),
})

type GroupValues = z.infer<typeof groupSchema>

const groupFields = ['name', 'contact_ids'] as const satisfies readonly FieldPath<GroupValues>[]

export function contactGroupBody(values: GroupValues): ContactGroupInput {
  return { name: values.name.trim(), contact_ids: values.contact_ids }
}

function AddIcon() {
  return <Icon name="add" />
}

/// 「联系人」标签：联系人与联系人组两张表。
export function ContactsPanel({ canManage }: { canManage: boolean }) {
  const contactsQuery = $api.useQuery('get', '/api/v1/notification-contacts')
  const groupsQuery = $api.useQuery('get', '/api/v1/notification-contact-groups')
  const deleteContactMutation = $api.useMutation('delete', '/api/v1/notification-contacts/{id}')
  const deleteGroupMutation = $api.useMutation('delete', '/api/v1/notification-contact-groups/{id}')
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const [contactEditor, setContactEditor] = useState<{ contact: Contact | null } | null>(null)
  const [groupEditor, setGroupEditor] = useState<{ group: ContactGroup | null } | null>(null)

  const contacts = contactsQuery.data ?? []
  const groups = groupsQuery.data ?? []
  const contactNames = new Map(contacts.map((contact) => [contact.id, contact.name]))
  const contactOptions: ContactOption[] = contacts.map((contact) => ({
    id: contact.id,
    label: `${contact.name} · ${contact.email}`,
  }))

  function removeContact(contact: Contact) {
    setFeedback(null)
    deleteContactMutation.mutate({ params: { path: { id: contact.id } } }, {
      onSuccess: () => {
        setFeedback({ tone: 'normal', text: '联系人已删除' })
        void contactsQuery.refetch()
        void groupsQuery.refetch()
      },
      onError: (error) => setFeedback({ tone: 'critical', text: apiErrorMessage(error, '删除联系人失败') }),
    })
  }

  function removeGroup(group: ContactGroup) {
    setFeedback(null)
    deleteGroupMutation.mutate({ params: { path: { id: group.id } } }, {
      onSuccess: () => {
        setFeedback({ tone: 'normal', text: '联系人组已删除' })
        void groupsQuery.refetch()
      },
      onError: (error) => setFeedback({ tone: 'critical', text: apiErrorMessage(error, '删除联系人组失败') }),
    })
  }

  return (
    <div className="alert-settings-stack">
      {feedback !== null && (
        <NotificationBar tone={feedback.tone} title={feedback.text} onClose={() => setFeedback(null)} />
      )}
      {contactsQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(contactsQuery.error, '联系人加载失败')} />
      )}
      <Panel
        flush
        title={`联系人（${contacts.length}）`}
        actions={<span title={canManage ? undefined : readOnlyReason.contacts}>
          <Button size="sm" renderIcon={AddIcon} disabled={!canManage} onClick={() => setContactEditor({ contact: null })}>
            新建联系人
          </Button>
        </span>}
      >
        <DataGrid<Contact>
          label="联系人"
          loading={contactsQuery.isPending}
          rows={contacts}
          rowKey={(contact) => contact.id}
          rowTestId="contact-row"
          columns={[
            { key: 'name', header: '姓名', minWidth: 160, grow: 1.3, cell: (contact) => <TruncatedText className="alert-settings-strong">{contact.name}</TruncatedText> },
            { key: 'email', header: '邮箱', minWidth: 220, cell: (contact) => <TruncatedText>{contact.email}</TruncatedText> },
            { key: 'external_id', header: '外部 ID', minWidth: 160, cell: (contact) => <TruncatedText>{contact.external_id ?? '—'}</TruncatedText> },
            {
              key: 'actions',
              header: '操作',
              minWidth: 96,
              grow: 1.6,
              align: 'end',
              cell: (contact) => (
                <span className="alert-settings-row-actions">
                  <InlineAction
                    name={`编辑 ${contact.name}`}
                    icon="edit"
                    disabled={!canManage}
                    disabledReason={readOnlyReason.contacts}
                    onClick={() => setContactEditor({ contact })}
                  />
                  <ConfirmedAction
                    name={`删除 ${contact.name}`}
                    icon="trashCan"
                    destructive
                    heading="删除联系人"
                    description={`删除后 ${contact.name} 会从所有联系人组与通知策略中移除，不再收到任何告警通知。此操作不可撤销。`}
                    confirmLabel="删除联系人"
                    disabled={!canManage}
                    disabledReason={readOnlyReason.contacts}
                    onConfirm={() => removeContact(contact)}
                  />
                </span>
              ),
            },
          ]}
          empty={{ title: '暂无联系人', description: '新建联系人后才能把他们编入联系人组与通知策略。' }}
        />
      </Panel>

      <Panel
        flush
        title={`联系人组（${groups.length}）`}
        actions={<span title={canManage ? undefined : readOnlyReason.contacts}>
          <Button size="sm" renderIcon={AddIcon} disabled={!canManage} onClick={() => setGroupEditor({ group: null })}>
            新建联系人组
          </Button>
        </span>}
      >
        <DataGrid<ContactGroup>
          label="联系人组"
          loading={groupsQuery.isPending}
          rows={groups}
          rowKey={(group) => group.id}
          rowTestId="contact-group-row"
          columns={[
            { key: 'name', header: '名称', minWidth: 180, grow: 1.3, cell: (group) => <TruncatedText className="alert-settings-strong">{group.name}</TruncatedText> },
            { key: 'size', header: '成员数', minWidth: 96, numeric: true, grow: 1.5, cell: (group) => group.contact_ids.length },
            {
              key: 'members',
              header: '成员',
              minWidth: 260,
              cell: (group) => (
                <TruncatedText>
                  {group.contact_ids.map((id) => contactNames.get(id) ?? id).join('、') || '—'}
                </TruncatedText>
              ),
            },
            {
              key: 'actions',
              header: '操作',
              minWidth: 96,
              grow: 1.6,
              align: 'end',
              cell: (group) => (
                <span className="alert-settings-row-actions">
                  <InlineAction
                    name={`编辑 ${group.name}`}
                    icon="edit"
                    disabled={!canManage}
                    disabledReason={readOnlyReason.contacts}
                    onClick={() => setGroupEditor({ group })}
                  />
                  <ConfirmedAction
                    name={`删除 ${group.name}`}
                    icon="trashCan"
                    destructive
                    heading="删除联系人组"
                    description={`删除后引用 ${group.name} 的通知策略会失去这一组收件人，组内联系人本身保留。此操作不可撤销。`}
                    confirmLabel="删除联系人组"
                    disabled={!canManage}
                    disabledReason={readOnlyReason.contacts}
                    onConfirm={() => removeGroup(group)}
                  />
                </span>
              ),
            },
          ]}
          empty={{ title: '暂无联系人组', description: '把联系人编成组，通知策略就不用逐个点名。' }}
        />
      </Panel>

      {contactEditor !== null && (
        <ContactModal
          contact={contactEditor.contact}
          onClose={() => setContactEditor(null)}
          onSaved={(message) => {
            setContactEditor(null)
            setFeedback({ tone: 'normal', text: message })
            void contactsQuery.refetch()
          }}
        />
      )}
      {groupEditor !== null && (
        <ContactGroupModal
          group={groupEditor.group}
          contactOptions={contactOptions}
          onClose={() => setGroupEditor(null)}
          onSaved={(message) => {
            setGroupEditor(null)
            setFeedback({ tone: 'normal', text: message })
            void groupsQuery.refetch()
          }}
        />
      )}
    </div>
  )
}

function ContactModal({ contact, onClose, onSaved }: {
  contact: Contact | null
  onClose: () => void
  onSaved: (message: string) => void
}) {
  const createMutation = $api.useMutation('post', '/api/v1/notification-contacts')
  const updateMutation = $api.useMutation('put', '/api/v1/notification-contacts/{id}')
  const [failure, setFailure] = useState('')
  const { formState, handleSubmit, register, setError } = useForm<ContactValues>({
    resolver: zodResolver(contactSchema),
    defaultValues: {
      name: contact?.name ?? '',
      email: contact?.email ?? '',
      external_id: contact?.external_id ?? '',
    },
  })

  const submit = handleSubmit((values) => {
    setFailure('')
    const options = {
      onSuccess: () => onSaved(contact === null ? '联系人已创建' : '联系人已更新'),
      onError: (error: unknown) => {
        if (applyApiFieldErrors<ContactValues>(error, contactFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '保存联系人失败'))
        }
      },
    }
    if (contact !== null) {
      updateMutation.mutate({ params: { path: { id: contact.id } }, body: contactBody(values) }, options)
      return
    }
    createMutation.mutate({ body: contactBody(values) }, options)
  })

  return (
    <Modal
      open
      modalHeading={contact === null ? '新建联系人' : '编辑联系人'}
      primaryButtonText="保存联系人"
      secondaryButtonText="取消"
      primaryButtonDisabled={createMutation.isPending || updateMutation.isPending}
      size="sm"
      onRequestSubmit={() => void submit()}
      onRequestClose={onClose}
      onSecondarySubmit={onClose}
    >
      <form className="alert-settings-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
        <FormField label="姓名" required errorText={formState.errors.name?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('name')}
          />}
        </FormField>
        <FormField label="邮箱" required errorText={formState.errors.email?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            type="email"
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('email')}
          />}
        </FormField>
        <FormField label="外部 ID" helperText="对接外部系统时的标识，可留空。" errorText={formState.errors.external_id?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('external_id')}
          />}
        </FormField>
      </form>
    </Modal>
  )
}

function ContactGroupModal({ group, contactOptions, onClose, onSaved }: {
  group: ContactGroup | null
  contactOptions: ContactOption[]
  onClose: () => void
  onSaved: (message: string) => void
}) {
  const createMutation = $api.useMutation('post', '/api/v1/notification-contact-groups')
  const updateMutation = $api.useMutation('put', '/api/v1/notification-contact-groups/{id}')
  const [failure, setFailure] = useState('')
  const { control, formState, handleSubmit, register, setError } = useForm<GroupValues>({
    resolver: zodResolver(groupSchema),
    defaultValues: { name: group?.name ?? '', contact_ids: group?.contact_ids ?? [] },
  })

  const submit = handleSubmit((values) => {
    setFailure('')
    const options = {
      onSuccess: () => onSaved(group === null ? '联系人组已创建' : '联系人组已更新'),
      onError: (error: unknown) => {
        if (applyApiFieldErrors<GroupValues>(error, groupFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '保存联系人组失败'))
        }
      },
    }
    if (group !== null) {
      updateMutation.mutate({ params: { path: { id: group.id } }, body: contactGroupBody(values) }, options)
      return
    }
    createMutation.mutate({ body: contactGroupBody(values) }, options)
  })

  return (
    <Modal
      open
      modalHeading={group === null ? '新建联系人组' : '编辑联系人组'}
      primaryButtonText="保存联系人组"
      secondaryButtonText="取消"
      primaryButtonDisabled={createMutation.isPending || updateMutation.isPending}
      size="sm"
      onRequestSubmit={() => void submit()}
      onRequestClose={onClose}
      onSecondarySubmit={onClose}
    >
      <form className="alert-settings-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
        <FormField label="名称" required errorText={formState.errors.name?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('name')}
          />}
        </FormField>
        <FormField label="成员" errorText={formState.errors.contact_ids?.message}>
          {(field) => <Controller
            name="contact_ids"
            control={control}
            render={({ field: members }) => <MultiSelect<ContactOption>
              id={field.id}
              titleText=""
              hideLabel
              label="选择联系人"
              items={contactOptions}
              itemToString={(item) => item?.label ?? ''}
              selectedItems={contactOptions.filter((option) => members.value.includes(option.id))}
              invalid={field.invalid}
              aria-describedby={field.describedBy}
              onChange={({ selectedItems }) => members.onChange((selectedItems ?? []).map((item) => item.id))}
            />}
          />}
        </FormField>
      </form>
    </Modal>
  )
}
