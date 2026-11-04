export async function api<T = any>(path: string, method = 'GET', body?: unknown): Promise<T> {
 const controller = new AbortController()
 const timer = setTimeout(() => controller.abort(), 9 * 60 * 1000)
 try {
  const response = await fetch(`/api${path}`, { method, signal: controller.signal, headers: body instanceof FormData ? {} : { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : body instanceof FormData ? body : JSON.stringify(body) })
  const value = await response.json().catch(() => ({ error: `服务返回异常（${response.status}）` }))
  if (!response.ok) throw new Error(value.error || `请求失败（${response.status}）`)
  return value as T
 } finally { clearTimeout(timer) }
}
export const formatMoney = (value: unknown) => value === null || value === undefined ? '—' : Number(value).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
