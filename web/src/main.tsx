import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { ConfigProvider } from 'antd'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { instanceRoute } from './routes/instances.$id'
import { instancesRoute } from './routes/instances'
import { loginRoute } from './routes/login'
import { rootRoute } from './routes/root'
import { usersRoute } from './routes/users'
import './styles.css'

const routeTree = rootRoute.addChildren([loginRoute, instancesRoute, instanceRoute, usersRoute])
const router = createRouter({ routeTree, defaultPreload: 'intent' })
const queryClient = new QueryClient()

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode><ConfigProvider><QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider></ConfigProvider></React.StrictMode>,
)
