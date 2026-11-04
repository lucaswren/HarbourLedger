import test from 'node:test'
import assert from 'node:assert/strict'
import { effectiveCategory, isOperatingCash, invoiceDirection, completedRow, pendingRow, inspectionBucket, inspectionResult, inspectionReason } from '../src/ledger-view.ts'

test('verified Stripe withdrawal is not missing a sales invoice or new operating revenue', () => {
 const row={stripe_payout_verified:true,category:'Stripe提现到账',state:'银行凭证已具备',dep:1195.70}
 assert.equal(inspectionBucket(row),'exempt')
 assert.equal(inspectionResult(row),'提现已核对')
 assert.equal(completedRow(row),true)
 assert.equal(isOperatingCash(row),false)
})

test('manual category overrides bank suggestion in cash statistics', () => {
 const row = { category: '资源采购成本', manual_category: '董事往来' }
 assert.equal(effectiveCategory(row), '董事往来')
 assert.equal(isOperatingCash(row), false)
 assert.equal(isOperatingCash({ category: '董事往来', manual_category: '资源采购成本' }), true)
 assert.equal(isOperatingCash({ transfer_verified: true, category: '资源采购成本' }), false)
})

test('invoice direction follows current selection when corrected', () => {
 assert.equal(invoiceDirection({ kind: '销售发票', direction: '发票' }), '发票')
 assert.equal(invoiceDirection({ kind: '销售发票' }), '销售发票')
 assert.equal(invoiceDirection({ kind: '待分类发票' }), '')
})

test('verification does not mean payment matched; partial payment stays pending', () => {
 assert.equal(completedRow({ verified: true, match_state: '未匹配流水' }), false)
 assert.equal(pendingRow({ state: '部分付款已关联，仍需补凭证' }), true)
 assert.equal(pendingRow({ status: '已确认', validity: '分配币种不符' }), true)
 assert.equal(completedRow({ match_state: '已匹配', duplicate: 'another' }), false)
 assert.equal(pendingRow({ status: '不匹配', validity: '不匹配' }), false)
})

test('inspection buckets separate matched, missing, director, exempt and unresolved',()=>{
 assert.equal(inspectionBucket({state:'发票已关联（复核分类）'}),'matched')
 assert.equal(inspectionBucket({state:'银行凭证已具备',category:'银行利息'}),'exempt')
 assert.equal(inspectionBucket({category:'董事往来',state:'银行凭证已具备'}),'director')
 assert.equal(inspectionBucket({category:'董事往来',manual_category:'服务收入',review_type:'未找到銷售憑證'}),'missing')
 assert.equal(inspectionBucket({review_type:'已找到票據'}),'pending')
 assert.equal(inspectionResult({wd:100,review_type:'未找到採購憑證'}),'缺採購發票')
 assert.equal(inspectionResult({state:'部分付款已关联，仍需补凭证'}),'部分匹配')
 assert.equal(inspectionReason({state:'发票已关联（复核分类）',review_reason:'old warning'}),'')
 assert.equal(inspectionResult({advice_sha:'original'}),'待補結算')
})
