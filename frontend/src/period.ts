import type { LedgerData } from './types'

export function currentMonth(date = new Date()): string {
 return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`
}

export function ledgerPeriod(data: Pick<LedgerData, 'checks' | 'tx' | 'invoices' | 'sales' | 'unclassified'>, fallback = currentMonth()): [string, string] {
 const months = [
  ...data.checks.map(row => String(row.period || '')),
  ...data.tx.map(row => String(row.period || '')),
  ...[...data.invoices, ...data.sales, ...data.unclassified].map(row => String(row.date || '').slice(0, 7)),
 ].filter(month => /^\d{4}-(0[1-9]|1[0-2])$/.test(month)).sort()
 return [months[0] || fallback, months[months.length - 1] || fallback]
}
