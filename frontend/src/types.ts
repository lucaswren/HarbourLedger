// SQLite records retain the original audit metadata across migrations.
export interface Row { [key: string]: any; id?: string; document_sha?: string }
export interface LedgerData { unclassified: Row[]; platforms: Row[]; sales: Row[]; tx: Row[]; checks: Row[]; invoices: Row[]; matches: Row[]; docs: Row[]; issued: Row[] }
export interface Column { key: string; label: string; width?: number; money?: boolean }
export type View = 'overview' | 'needs' | 'tx' | 'invoices' | 'matches' | 'checks' | 'docs' | 'sales' | 'unclassified'
export const allViews: { key: View; title: string; subtitle: string }[] = [
 { key: 'overview', title: '整理概覽', subtitle: '整理銀行流水、銷售及採購憑證，核對收付款、匹配及待處理事項。' },
 { key: 'needs', title: '憑證核查', subtitle: '真缺票、已有票據待匹配及董事往來分開顯示。' },
 { key: 'tx', title: '銀行流水', subtitle: '查看實際收付款，保存分類與復核說明。' },
 { key: 'invoices', title: '採購憑證', subtitle: '發票或付款收據均可導入，按原幣金額匹配付款。' },
 {key:'unclassified',title:'待分類發票',subtitle:'未能確認本公司為開票方或買方，請核對後確認銷售或採購。'},
 {key:'sales',title:'銷售憑證',subtitle:'整理銷售發票及收據，核對客戶、原幣金額與實際收款。'},
 { key: 'matches', title: '收付款關聯', subtitle: '逐筆復核分配依據，保留確認人和跨幣憑證說明。' },
 { key: 'checks', title: '銀行月結單', subtitle: '核對金額、筆數、余額及相鄰月份銜接。' },
 { key: 'docs', title: '憑證文件', subtitle: '按年份及類別整理、命名與清理重複文件。' },
]
export const views = allViews
export const columns: Record<Exclude<View, 'overview'>, Column[]> = {
 unclassified: [{key:'date',label:'日期',width:120},{key:'number',label:'發票編號',width:180},{key:'seller',label:'開票方',width:260},{key:'buyer',label:'買方',width:260},{key:'ccy',label:'幣種',width:80},{key:'amount',label:'金額',money:true,width:130},{key:'direction_basis',label:'待核對原因',width:350}],
 sales: [{key:'service_period',label:'服務期間',width:230},{key:'match_state',label:'流水匹配',width:160},{key:'date',label:'開票日期',width:115},{key:'customer',label:'客戶',width:260},{key:'number',label:'銷售發票編號',width:190},{key:'ccy',label:'幣種',width:75},{key:'amount',label:'金額',money:true,width:130},{key:'due_date',label:'到期日',width:115},{key:'note',label:'備注',width:300}],
 needs: [{key:'date',label:'日期',width:105},{key:'party',label:'交易對象',width:170},{key:'ccy',label:'幣種',width:85},{key:'flow_amount',label:'收款 / 付款',width:120,money:true},{key:'inspection_result',label:'核查結果',width:145},{key:'display_category',label:'分類',width:160}],
 tx: [{key:'date',label:'日期',width:115},{key:'account',label:'賬戶',width:205},{key:'ccy',label:'幣種',width:75},{key:'dep',label:'收款',width:130,money:true},{key:'wd',label:'付款',width:130,money:true},{key:'balance',label:'銀行余額',width:145,money:true},{key:'fx_status',label:'跨幣核對',width:250},{key:'fx',label:'銀行匯率',width:125},{key:'orig_ccy',label:'原幣',width:75},{key:'orig_amount',label:'原幣金額',width:135,money:true},{key:'display_category',label:'分類',width:200},{key:'details',label:'銀行摘要',width:450},{key:'manual_note',label:'復核說明',width:260}],
 invoices: [{key:'document_type_label',label:'憑證類型',width:125},{key:'service_period',label:'服務期間',width:230},{key:'match_state',label:'流水匹配',width:160},{key:'date',label:'發票日期',width:115},{key:'vendor',label:'供應商',width:290},{key:'number',label:'發票編號',width:180},{key:'ccy',label:'幣種',width:75},{key:'amount',label:'金額',width:140,money:true},{key:'duplicate',label:'重復憑證',width:185},{key:'note',label:'備註',width:340}],
 matches: [{key:'bank_ccy',label:'銀行幣種',width:95},{key:'bank_amount',label:'銀行收付款',width:145,money:true},{key:'orig_ccy',label:'銀行原幣',width:95},{key:'orig_amount',label:'原幣金額',width:135,money:true},{key:'fx_basis',label:'跨幣匹配依據',width:350},{key:'payment_date',label:'收付款日期',width:115},{key:'invoice_number',label:'發票編號',width:190},{key:'ccy',label:'幣種',width:75},{key:'amount',label:'分配金額',width:140,money:true},{key:'status',label:'確認狀態',width:115},{key:'validity',label:'校驗結果',width:210},{key:'reviewer',label:'復核人',width:120},{key:'basis',label:'候選依據',width:350},{key:'evidence',label:'跨幣憑證說明',width:300}],
 checks: [{key:'period',label:'月份',width:105},{key:'bank_number',label:'銀行賬號',width:180},{key:'account',label:'賬戶',width:210},{key:'ccy',label:'幣種',width:75},{key:'opening',label:'期初余額',width:145,money:true},{key:'dep',label:'收款',width:135,money:true},{key:'wd',label:'付款',width:135,money:true},{key:'closing',label:'期末余額',width:145,money:true},{key:'delta',label:'余額差額',width:125,money:true},{key:'continuity',label:'相鄰月差額',width:135,money:true},{key:'check_status',label:'對賬狀態',width:200}],
 docs: [{key:'kind',label:'類型',width:140},{key:'pdf_type_label',label:'PDF類型',width:210},{key:'archive_name',label:'自動歸檔名稱',width:450},{key:'direction_basis',label:'分類依據',width:320},{key:'path',label:'歸檔路徑',width:650}],
}
