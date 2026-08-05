import { createRoute, useNavigate } from '@tanstack/react-router'
import { Alert, Button, Card, Form, Input, Typography } from 'antd'
import { useState } from 'react'
import { rootRoute } from './root'

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
})

function LoginPage() {
  const navigate = useNavigate()
  const [error, setError] = useState('')

  async function login(values: { username: string; password: string }) {
    setError('')
    const response = await fetch('/api/v1/login', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin',
      body: JSON.stringify(values),
    })
    if (!response.ok) {
      setError('用户名或密码错误')
      return
    }
    await navigate({ to: '/instances' })
  }

  return <Card style={{ maxWidth: 420, margin: '80px auto' }}><Typography.Title level={2}>登录</Typography.Title>{error && <Alert type="error" message={error} />}<Form layout="vertical" onFinish={login}><Form.Item name="username" label="用户名" rules={[{ required: true }]}><Input autoComplete="username" /></Form.Item><Form.Item name="password" label="密码" rules={[{ required: true }]}><Input.Password autoComplete="current-password" /></Form.Item><Button type="primary" htmlType="submit">登录</Button></Form></Card>
}
