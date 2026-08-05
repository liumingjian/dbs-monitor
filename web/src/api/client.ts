import createFetchClient from 'openapi-fetch'
import createQueryClient from 'openapi-react-query'
import type { paths } from './schema'

const fetchClient = createFetchClient<paths>({
  baseUrl: '/',
  credentials: 'same-origin',
  fetch: async (request) => {
    const response = await fetch(request)
    if (response.status === 401 && window.location.pathname !== '/login') {
      window.location.assign('/login')
    }
    return response
  },
})

export const $api = createQueryClient(fetchClient)
