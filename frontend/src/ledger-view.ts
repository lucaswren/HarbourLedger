import type { Row } from './types'

export function effectiveCategory(row: Row): string {
 return String(row.manual_category || row.category || '')
}

export function isOperatingCash(row: Row): boolean {
 return !row.transfer_verified && !row.stripe_payout_verified && !/内部|內部|董事/.test(effectiveCategory(row))
}

export function invoiceDirection(row: Row): string {
 const direction = row.direction || row.kind
 return ['发票', '销售发票'].includes(String(direction)) ? String(direction) : ''
}

export function completedRow(row: Row): boolean {
 if (row.duplicate) return false
 if (row.match_state) return row.match_state === '已匹配'
 if (row.validity) return row.validity === '有效确认' || row.status === '不匹配'
 if (typeof row.ok === 'boolean') return row.ok
 return ['银行凭证已具备', '发票已关联（复核分类）'].includes(String(row.state))
}

export function pendingRow(row: Row): boolean {
 if (row.duplicate) return true
 if (row.match_state || row.validity || typeof row.ok === 'boolean' || row.state) return !completedRow(row)
 return row.status === '待确认' || row.kind === '待分类发票'
}

export type InspectionBucket = 'matched' | 'missing' | 'director' | 'pending' | 'exempt'
export const inspectionLabels: Record<InspectionBucket,string> = {matched:'已匹配',missing:'缺票',director:'董事往來',pending:'待處理',exempt:'無需發票'}
export function inspectionBucket(row: Row): InspectionBucket {
 if (/董事/.test(effectiveCategory(row))) return 'director'
 if (row.state === '发票已关联（复核分类）') return 'matched'
 if (row.state === '银行凭证已具备') return 'exempt'
 if (['未找到採購憑證','未找到銷售憑證'].includes(String(row.review_type))) return 'missing'
 return 'pending'
}
export function inspectionResult(row: Row): string {
 if(row.stripe_payout_verified)return '提现已核对'
 const bucket=inspectionBucket(row)
 if(bucket==='missing')return row.wd>0?'缺採購發票':'缺銷售發票'
 if(bucket==='pending')return row.advice_sha?'待補結算':row.state==='部分付款已关联，仍需补凭证'?'部分匹配':'待匹配'
 return inspectionLabels[bucket]
}
export function inspectionReason(row: Row): string {
 if(inspectionBucket(row)!=='pending')return ''
 if(row.advice_sha)return '補充傭金或平台結算資料'
 if(row.state==='部分付款已关联，仍需补凭证')return '尚有未分配金額'
 return String(row.review_reason||'')
}
