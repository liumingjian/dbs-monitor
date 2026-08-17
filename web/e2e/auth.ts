export const STORAGE_STATE_PATH = 'e2e/.auth/admin.json'

// 给自己走 UI 登录流程的 spec 用：清空预热的会话，从未登录状态开始。
export const EMPTY_STORAGE_STATE = { cookies: [], origins: [] }
