import { Button, TextInput } from '@carbon/react'
import { createRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import type { operations } from '../api/schema'
import { zodResolver } from '../forms/zodResolver'
import { FormField } from '../primitives/FormField'
import { NotificationBar } from '../primitives/NotificationBar'
import { Panel } from '../primitives/Panel'
import { rootRoute } from './root'
import './login.css'

type LoginInput = operations['createSession']['requestBody']['content']['application/json']

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
})

/// 登录表单的校验规则。空表单在浏览器里就被挡下来，不用往服务端跑一趟才知道少填了东西。
/// 与生成的请求体类型对齐：`satisfies z.ZodType<LoginInput>` + `loginBody` 把出参当请求体用。
const loginSchema = z.object({
  username: z.string().refine((value) => value.trim() !== '', '请输入用户名'),
  password: z.string().min(1, '请输入密码'),
}) satisfies z.ZodType<LoginInput>

type LoginValues = z.infer<typeof loginSchema>

function loginBody(values: LoginValues): LoginInput {
  return { username: values.username.trim(), password: values.password }
}

/// 登录页。
///
/// 它在应用外框之外（`root/index.tsx` 里 `/login` 不渲染侧栏），所以自己负责居中版式。
///
/// 登录仍然走裸 `fetch` 而不是 `$api`：这一发请求的意义就是把会话 cookie 换回来，
/// 它没有查询缓存可言，成功之后立刻整页换路由。
///
/// 认证失败**不是**字段错误：服务端不会（也不该）告诉调用方错的是用户名还是密码，
/// 所以它落在整表单的错误条上，而不是某个输入框下面。
function LoginPage() {
  const navigate = useNavigate()
  const { formState, handleSubmit, register, setValue } = useForm<LoginValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: '', password: '' },
  })
  const [failure, setFailure] = useState('')

  const submit = handleSubmit(async (values) => {
    setFailure('')
    const response = await fetch('/api/v1/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify(loginBody(values)),
    })
    if (!response.ok) {
      setFailure('用户名或密码错误')
      setValue('password', '')
      return
    }
    // 登录后落地到机群总览：先回答「整体还好吗」，再由读者决定看谁。
    await navigate({ to: '/' })
  })

  return (
    <div className="login-page">
      <Panel className="login-panel" title="登录" description="使用平台账号登录 DBS Monitor">
        <form className="login-form" onSubmit={(event) => void submit(event)} noValidate>
          {failure !== '' && <NotificationBar tone="critical" title={failure} />}
          <FormField label="用户名" required errorText={formState.errors.username?.message}>
            {(field) => <TextInput
              id={field.id}
              labelText=""
              hideLabel
              autoComplete="username"
              invalid={field.invalid}
              aria-describedby={field.describedBy}
              {...register('username')}
            />}
          </FormField>
          <FormField label="密码" required errorText={formState.errors.password?.message}>
            {(field) => <TextInput
              id={field.id}
              type="password"
              labelText=""
              hideLabel
              autoComplete="current-password"
              invalid={field.invalid}
              aria-describedby={field.describedBy}
              {...register('password')}
            />}
          </FormField>
          <Button type="submit" size="lg" disabled={formState.isSubmitting}>登录</Button>
        </form>
      </Panel>
    </div>
  )
}
