<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import zhTw from 'element-plus/es/locale/lang/zh-tw'
import zhHans from 'element-plus/es/locale/lang/zh-cn'
import enLocale from 'element-plus/es/locale/lang/en'
import {t as tr, language} from './language'
import { Collection, Document, Download, Wallet, FolderOpened, Grid, Link, List, Plus, Refresh, Search, Upload, CircleCheck, Warning, Setting } from '@element-plus/icons-vue'
import { api, formatMoney } from './api'
import LedgerSettings from './LedgerSettings.vue'
import { companySettings, type CompanySettings } from './settings'
import { currentMonth, ledgerPeriod } from './period'
import { effectiveCategory, isOperatingCash, invoiceDirection, completedRow, pendingRow, inspectionBucket, inspectionResult, inspectionReason, inspectionLabels, type InspectionBucket } from './ledger-view'
import { columns, views, allViews, type LedgerData, type Row, type View } from './types'

const uiLocale=computed(()=>language.value==='en'?enLocale:language.value==='zh-Hans'?zhHans:zhTw)
const categoryOptions=['销售及服务收入','采购及服务成本','广告推广费','银行费用','银行利息','董事往来','内部转账/换汇','行政及登记费','推薦佣金收入','其他服务支出','待会计师分类']
const data = ref<LedgerData>({ unclassified: [], platforms: [], sales: [], tx: [], checks: [], invoices: [], matches: [], docs: [], issued: [] })
const active = ref<View>(allViews.some(v=>v.key===location.hash.slice(1))?location.hash.slice(1) as View:'overview'), busy = ref(false), loadingText = ref('正在加载本地账务…'), error = ref(''), query = ref(''), page = ref(1), pageSize = ref(30), statusFilter = ref('全部')
function previousMonth(){const [y,m]=currentMonth().split('-').map(Number);return new Date(Date.UTC(y!,m!-2,1)).toISOString().slice(0,7)}
const period = ref<[string, string]>([previousMonth(),previousMonth()]), periodMode = ref('全部月份'), year = ref(new Date().getFullYear()), month = ref(new Date().getMonth() + 1), quarter = ref(Math.ceil((new Date().getMonth()+1)/3))
function applyDefaultPeriod() { period.value=ledgerPeriod(data.value) }
const preferencesLoaded=ref(false)
watch(language,v=>{if(preferencesLoaded.value)void api('/settings','PUT',{ui_language:v}).catch(()=>ElMessage.error(tr('語言設定未保存')))})
async function refreshConnectionStatus(){ocrStatus.value=await api<Row>('/ocr/status')}
const ocrStatus = ref<Row>({ configured:false, mode:'本地PDF解析', partial_configuration:false })
function newPlatform(row?:Row){Object.keys(form).forEach(k=>delete form[k]);Object.assign(form,{id:'',tx:row?.id||'',payout_id:row?`BANK-${row.id}`:'',ccy:row?.ccy||'USD',gross:row?.dep||0,fees:0,refunds:0,adjustments:0,status:'待复核',reviewer:'',evidence_sha:'',settlement_kind:'跨境匯款',fee_basis:'',invoice:''});dialog.value='platform'}
function chooseSettlementInvoice(id:string){const i=data.value.sales.find(i=>i.id===id),t=data.value.tx.find(t=>t.id===form.tx);if(i){form.gross=i.amount;form.ccy=i.ccy}if(i&&t&&i.ccy===t.ccy)form.fees=Math.max(0,Math.round((Number(i.amount)-Number(t.dep))*100)/100)}
const settlementNet=computed(()=>Number(form.gross||0)-Number(form.fees||0)-Number(form.refunds||0)+Number(form.adjustments||0))
async function setCategory(row:Row,category:string){await run('正在保存分類…',async()=>{await api(`/reviews/${row.id}`,'PUT',{category,note:row.manual_note||''});await load()})}

const sort = reactive({prop:'',order:''})
const previewBlob=ref('')
const restoreInput=ref<HTMLInputElement|null>(null)
const ledgerSettings = reactive(companySettings())
const company = computed(() => ledgerSettings.company)
const allowIncomplete = ref(false), settingsOpen = ref(false), exportOpen = ref(false), importOpen = ref(false), importKind = ref('银行'), importFiles = ref<File[]>([]), importResults = ref<Row | null>(null)
const exportMonths=ref<[string,string]>([previousMonth(),previousMonth()])
const exportPlatformPDFs = ref<string[]>([])
const platformPDFs = computed(() => data.value.docs.filter(d => d.kind === "平台结算"))
watch(exportOpen,open=>{if(open){exportMonths.value=[...period.value];allowIncomplete.value=false;exportPlatformPDFs.value=[]}})
const dialog = ref(''), form = reactive<Row>({}), formError = ref(''), previewSha = ref(''), previewOpen = ref(false), latestDownload = ref<Row | null>(null)
const accounts = ref<Row[]>([]), accountOpen = ref(false), accountForm = reactive<Row>({})
const organizeResult = ref<Row | null>(null)
const rebuildOpen=ref(false), rebuildCloud=ref(false), rebuildJob=ref<Row>({status:'idle'}), rebuildResult=ref<Row|null>(null)
async function monitorRebuild(id:string|undefined) {
 if(!id) throw new Error('梳理任务编号缺失，请刷新后重试')
 for (;;) {
  const job=await api<Row>('/rebuild/status');rebuildJob.value=job
  if(job.id!==id) throw new Error('梳理任务已变化，请刷新查看状态')
  loadingText.value=`正在梳理全部凭证：${job.done}/${job.total} ${job.name || '校验与备份中'}`
  if(job.status==='completed') {rebuildResult.value=job.result;return}
  if(job.status==='failed') throw new Error(job.error || '账务梳理失败')
  await new Promise(resolve=>setTimeout(resolve,1000))
 }
}
async function rebuildLedger() {await run('正在启动账务梳理…',async()=> {
 rebuildResult.value=null
 const job=await api<Row>('/rebuild','POST',{use_cloud:rebuildCloud.value})
 await monitorRebuild(job.id)
 await load();ElMessage.success(tr('全部导入凭证已梳理，请查看逐文件结果'))
})}
async function organizeDocuments() { await run('正在校验文件内容、备份并整理凭证…',async()=> {
 organizeResult.value=await api<Row>('/documents/organize','POST',{})
 await load()
 ElMessage.success(tr(`已按内容整理：规范命名 ${organizeResult.value.renamed} 份，清理重复副本 ${organizeResult.value.removed} 份`))
}) }
function editAccount(row?: Row) {
 Object.keys(accountForm).forEach(k=>delete accountForm[k])
 Object.assign(accountForm, row ? JSON.parse(JSON.stringify(row)) : {id:'',bank_name:'',bank_number:'',account:'多币种综合账户',currencies:['HKD','USD'],opened_on:'',closed_on:'',status:'待确认',note:''})
 accountOpen.value=true
}
async function saveAccount() { await run('正在保存账户基础信息…', async()=> {
 await api('/accounts','POST',accountForm)
 accounts.value=await api<Row[]>('/accounts');accountOpen.value=false;ElMessage.success(tr('账户信息已保存'))
}) }
const visibleViews=computed(()=>views.filter(v=>!['matches','docs'].includes(v.key)&&(v.key!=='unclassified'||data.value.unclassified.length>0)))
const detailViews=views.filter(v=>['matches','docs'].includes(v.key))
function showGaps(_scope='全部', filter='全部') {
 active.value='needs'
 gapFilter.value = filter==='缺票'||filter.startsWith('未找到')?'missing':filter.includes('董事')?'director':filter==='已匹配'?'matched':filter==='無需發票'?'exempt':filter==='全部'?'全部':'pending'
}
const financeStats=computed(()=>{
 const groups=new Map<string,{ccy:string,incoming:number,outgoing:number,operatingIn:number,operatingOut:number,count:number}>()
 for(const t of transactions.value){
  const ccy=String(t.ccy||'');if(!ccy)continue
  const row=groups.get(ccy)||{ccy,incoming:0,outgoing:0,operatingIn:0,operatingOut:0,count:0}
  const incoming=Math.round(Number(t.dep||0)*100),outgoing=Math.round(Number(t.wd||0)*100)
  row.incoming+=incoming;row.outgoing+=outgoing;row.count++
  if(isOperatingCash(t)){row.operatingIn+=incoming;row.operatingOut+=outgoing}
  groups.set(ccy,row)
 }
 return [...groups.values()].sort((a,b)=>a.ccy.localeCompare(b.ccy))
})
const directorDocOpen=ref(false),directorDocResult=ref<Row|null>(null)
const directorDocForm=reactive({kind:'borrow',tx_ids:[] as string[],terms:'',loan_reference:''})
const directorDocRows=computed(()=>directorTransactions.value.filter(t=>directorDocForm.kind==='borrow'?t.dep>0:t.wd>0))
function newDirectorDocument(){Object.assign(directorDocForm,{kind:'borrow',tx_ids:[],terms:'',loan_reference:''});directorDocResult.value=null;directorDocOpen.value=true}
watch(()=>directorDocForm.kind,()=>{directorDocForm.tx_ids=[];directorDocResult.value=null})
async function generateDirectorDocument(){await run('正在生成董事往來證明草稿…',async()=>{directorDocResult.value=await api<Row>('/director-documents','POST',directorDocForm);previewSha.value='';previewBlob.value=directorDocResult.value.url;previewOpen.value=true})}
function invoiceLinkNote(row:Row,i:Row){const m=data.value.matches.find(m=>m.tx===row.id&&m.invoice===i.id&&m.status!=='不匹配');if(!m)return '';if(m.validity==='有效确认')return m.bank_ccy!==i.ccy&&!row.orig_ccy?'跨幣種已關聯；金額無法直接比較':'已關聯';return '候選：'+String(m.validity||m.status)}
function linkedInvoices(row:Row){const ids=new Set(data.value.matches.filter(m=>m.tx===row.id&&m.status!=='不匹配').map(m=>m.invoice));return [...data.value.invoices,...data.value.sales].filter(i=>ids.has(i.id)&&!i.duplicate)}
const directorTransactions=computed(()=>transactions.value.filter(t=>/董事/.test(effectiveCategory(t))))
const directorStats=computed(()=>{
 const groups=new Map<string,{ccy:string,incoming:number,outgoing:number,count:number}>()
 for(const t of directorTransactions.value){
  const ccy=String(t.ccy||'');if(!ccy)continue
  const row=groups.get(ccy)||{ccy,incoming:0,outgoing:0,count:0}
  row.incoming+=Math.round(Number(t.dep||0)*100);row.outgoing+=Math.round(Number(t.wd||0)*100);row.count++
  groups.set(ccy,row)
 }
 return [...groups.values()].sort((a,b)=>a.ccy.localeCompare(b.ccy))
})
const icons = {overview:Grid,needs:Warning,tx:List,invoices:Document,sales:Document,unclassified:Warning,checks:CircleCheck,matches:Link,docs:FolderOpened}
const selectedView = computed(() => allViews.find(v => v.key === active.value)!)
const inPeriod = (value: string) => !!period.value?.[0] && !!period.value?.[1] && value >= period.value[0] && value <= period.value[1]
const transactions = computed(() => data.value.tx.filter(t => inPeriod(t.period)))
const txIds = computed(() => new Set(transactions.value.map(t => t.id)))
const txMap = computed(() => new Map(data.value.tx.map(t => [t.id, t])))
const invMap = computed(() => new Map([...data.value.invoices,...data.value.sales].map(i => [i.id, i])))
const matches = computed(() => data.value.matches.filter(m => txIds.value.has(m.tx)))
const invoiceIds = computed(() => new Set(matches.value.filter(m=>m.status!=='不匹配').map(m => m.invoice)))
const invoices = computed(() => data.value.invoices.filter(i => !i.date || inPeriod(i.date.slice(0,7)) || invoiceIds.value.has(i.id)))
const inspectionRows=computed(()=>transactions.value.map(t=>({...t,flow_amount:t.wd||t.dep,display_category:effectiveCategory(t),inspection_bucket:inspectionBucket(t),inspection_result:inspectionResult(t),inspection_reason:inspectionReason(t)})))
const inspectionGroups=computed(()=>(Object.keys(inspectionLabels) as InspectionBucket[]).map(key=>({key,label:inspectionLabels[key],count:inspectionRows.value.filter(t=>t.inspection_bucket===key).length})))
const gapFilter=ref('全部')
const missingDocumentCount=computed(()=>inspectionRows.value.filter(t=>t.inspection_bucket==='missing').length)
function invoiceMatchState(i:Row){const links=data.value.matches.filter(m=>m.invoice===i.id);const valid=links.filter(m=>m.validity==='有效确认');const amount=valid.reduce((n,m)=>n+Math.round(Number(m.amount||0)*100),0);return amount>=Math.round(Number(i.amount||0)*100)&&amount>0?'已匹配':amount>0?'部分匹配':links.some(m=>m.status==='待确认')?'已找到流水':'未匹配流水'}
const checks = computed(() => data.value.checks.filter(c => inPeriod(c.period)))
const matchableInvoices = computed(() => [...data.value.invoices,...data.value.sales].filter(i => !i.duplicate && i.status!=='草稿' && (form.direction==='收款' ? i.kind==='销售发票' : i.kind!=='销售发票')))
const paymentSummary=computed(()=>{const t=txMap.value.get(form.tx);if(!t)return '';return `銀行${t.dep>0?'入賬':'扣賬'}：${t.ccy} ${formatMoney(t.wd||t.dep)}；銀行列示原幣：${t.orig_ccy ? t.orig_ccy+' '+formatMoney(t.orig_amount) : '未列示'}${t.fx?`；銀行匯率：${t.fx}`:''}。分配金額按發票幣種填寫；銀行扣賬與發票金額分別保留，無需另交換匯憑證。`})
const matchTransactions = computed(()=>data.value.tx.filter(t=>form.direction==='收款' ? t.dep>0 : t.wd>0))
const missingMonths = computed(() => {
 const [start,end] = period.value || []; if (!start || !end || start > end) return []
 const present = new Set(checks.value.map(c => c.period)), result: string[] = []
 const cursor = new Date(`${start}-01T00:00:00`), last = new Date(`${end}-01T00:00:00`)
 for (let n=0; cursor <= last && n<1201; n++) { const p = `${cursor.getFullYear()}-${String(cursor.getMonth()+1).padStart(2,'0')}`; if (!present.has(p)) result.push(p); cursor.setMonth(cursor.getMonth()+1) }
 return result
})
const exportChecks=computed(()=>data.value.checks.filter(c=>c.period>=exportMonths.value?.[0]&&c.period<=exportMonths.value?.[1]))
const exportMissingMonths=computed(()=>{const [start,end]=exportMonths.value||[];if(!start||!end)return [];const out:string[]=[];const seen=new Set(exportChecks.value.map(c=>c.period));const d=new Date(`${start}-01T00:00:00`),last=new Date(`${end}-01T00:00:00`);for(let n=0;d<=last&&n<1201;n++){const p=`${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}`;if(!seen.has(p))out.push(p);d.setMonth(d.getMonth()+1)};return out})
const rows = computed<Row[]>(() => {
 const tx: Row[] = transactions.value.map(t => ({...t, flow_amount:t.wd || t.dep, display_category:t.manual_category || t.category}))
 switch(active.value) {
 case 'needs': return inspectionRows.value.filter(t=>gapFilter.value==='全部'||t.inspection_bucket===gapFilter.value)
 case 'tx': return tx
 case 'unclassified': return data.value.unclassified
 case 'sales': return data.value.sales.filter(i=>!i.date||inPeriod(i.date.slice(0,7))||invoiceIds.value.has(i.id)).map(i=>({...i,document_type_label:i.document_type==='payment_receipt'?'付款收據':'發票',match_state:invoiceMatchState(i),verification:i.verified?'已核對':i.extraction_complete?'識別完整':'資料待補'}))
 case 'invoices': return invoices.value.map(i => ({...i, document_type_label:i.document_type==='payment_receipt'?'付款收據':'發票',match_state:invoiceMatchState(i), verification:i.amount>0 && i.ccy ? '金額已提取' : '資料待補'}))
 case 'matches': return matches.value.map(m => ({...m, payment_date:txMap.value.get(m.tx)?.date || '流水失效', invoice_number:invMap.value.get(m.invoice)?.number || m.invoice, document_sha:invMap.value.get(m.invoice)?.document_sha}))
 case 'checks': return checks.value.map(c => ({...c, check_status:!c.ok ? '核对失败' : c.opening == null ? '金额笔数通过，期初未提供' : '核对通过'}))
 case 'docs': return data.value.docs
 default: return []
 }
})
const filtered = computed(() => rows.value.filter(row => {
 const text = (Object.values(row).filter(v => typeof v === 'string' || typeof v === 'number').join(' ') + (['needs','tx'].includes(active.value)?JSON.stringify(linkedInvoices(row)):'')).toLowerCase()
 if (query.value && !text.includes(query.value.toLowerCase())) return false
 if (statusFilter.value === '待处理') return pendingRow(row)
 if (statusFilter.value === '已完成') return completedRow(row)
 return true
}))
const sortedRows = computed(() => { if(!sort.prop || !sort.order) return filtered.value; return [...filtered.value].sort((a,b) => {const x=a[sort.prop],y=b[sort.prop];const n=typeof x==='number' && typeof y==='number' ? x-y : String(x ?? '').localeCompare(String(y ?? ''),'zh-CN',{numeric:true});return sort.order==='ascending'?n:-n}) })
const pageRows = computed(() => sortedRows.value.slice((page.value-1)*pageSize.value, page.value*pageSize.value))
function sortChanged(v:{prop:string,order:string}) {sort.prop=v.prop;sort.order=v.order;page.value=1}
const currentColumns = computed(() => active.value === 'overview' ? [] : columns[active.value].filter(c=>!['service_period','due_date','duplicate','note','reference_note','inspection_reason'].includes(c.key)||rows.value.some(r=>!!r[c.key])))
watch([active, query, period, statusFilter, pageSize, gapFilter], () => { page.value = 1 })
watch(active, () => { history.replaceState(null,'','#'+active.value); gapFilter.value=active.value==='needs'?(missingDocumentCount.value?'missing':inspectionRows.value.some(t=>t.inspection_bucket==='pending')?'pending':'matched'):'全部';query.value=''; statusFilter.value='全部';sort.prop='';sort.order='' },{flush:'sync'})
watch([periodMode,year,month,quarter], () => {
 if(periodMode.value==='全部月份'){applyDefaultPeriod();return}
 if(periodMode.value === '自定义') return
 const y = year.value, pad = (m:number) => `${y}-${String(m).padStart(2,'0')}`
 period.value = periodMode.value === '月度' ? [pad(month.value),pad(month.value)] : periodMode.value === '季度' ? [pad((quarter.value-1)*3+1),pad(quarter.value*3)] : [pad(1),pad(12)]
})

async function load() { const [ledger, list] = await Promise.all([api<LedgerData>('/data'),api<Row[]>('/accounts')]);data.value=ledger;accounts.value=list;if(periodMode.value==='全部月份')applyDefaultPeriod() }


async function run(label: string, fn: () => Promise<void>) {
 if(busy.value) return
 busy.value=true; loadingText.value=label; error.value=''
 try { await fn() } catch(e) { error.value=e instanceof Error ? e.message : String(e); ElMessage.error({message:tr(error.value),duration:7000}) } finally { busy.value=false }
}
onMounted(() => run('正在读取本地账务…',async () => {
 const job=await api<Row>('/rebuild/status')
 if(['queued','running'].includes(job.status)) {rebuildOpen.value=true;await monitorRebuild(job.id)}
 else if(job.status==='completed') {rebuildResult.value=job.result;rebuildJob.value=job}
 const [settings,ocr] = await Promise.all([api<Row>('/settings'),api<Row>('/ocr/status')]);ocrStatus.value=ocr;if(['en','zh-Hant','zh-Hans'].includes(settings.ui_language))language.value=settings.ui_language;preferencesLoaded.value=true;Object.assign(ledgerSettings,companySettings(settings));
 periodMode.value='全部月份'
 if(settings.year)year.value=Number(settings.year);if(settings.month)month.value=Number(settings.month);if(settings.quarter)quarter.value=Number(settings.quarter)
 if(settings.start && settings.end) period.value=[settings.start,settings.end]
 await load()
}))
function openPreview(sha?: string) { if(sha) { previewBlob.value=''; previewSha.value=sha; previewOpen.value=true } }
function edit(row: Row) {
 Object.keys(form).forEach(k => delete form[k]); formError.value=''; Object.assign(form,JSON.parse(JSON.stringify(row)))
 if(active.value==='invoices'||active.value==='sales'||active.value==='unclassified') { dialog.value='invoice';form.direction=invoiceDirection(row);form.document_type=row.document_type||'invoice'; form.accounts_text=(row.accounts || []).join(', ') }
 else if(active.value==='matches'){dialog.value='match';form.direction=invMap.value.get(row.invoice)?.kind==='销售发票'?'收款':'付款'}
 else if(active.value==='tx' || active.value==='needs') { dialog.value='review'; form.category=row.manual_category || row.category; form.note=row.manual_note || '' }
 else openPreview(row.document_sha || row.sha)
}
function newMatch() { Object.keys(form).forEach(k=>delete form[k]);Object.assign(form,{id:'',tx:'',invoice:'',ccy:'HKD',amount:null,direction:'付款',basis:'人工关联',status:'待确认',reviewer:'',note:'',evidence:''});formError.value='';dialog.value='match' }
function matchPayment(row: Row) {
 newMatch()
 Object.assign(form, {direction:row.dep>0?'收款':'付款',tx:row.id, ccy:row.orig_ccy || row.ccy, amount:row.orig_amount || row.wd || row.dep})
}
const dialogTitle = computed(() => ({invoice:'票據資料',platform:'銀行手續費／收款差額',match:'收付款与发票分配',review:'流水分类与复核'}[dialog.value] || ''))
async function saveForm() { await run('正在校验并保存…',async () => {
 formError.value=''
 try {
  if(dialog.value==='platform'){await api('/platforms','POST',{...form})}
  else if(dialog.value==='invoice') { if(!form.date || !((form.direction==='销售发票')?form.customer:form.vendor)?.trim() || !form.number?.trim() || !form.amount || !form.ccy) throw new Error('请填写日期、供应商、编号、币种和正数金额'); await api(`/invoices/${form.id}`,'PUT',{...form,accounts:form.accounts_text.split(/[,，;；\n]/).map((s:string)=>s.trim()).filter(Boolean)}) }
  else if(dialog.value==='review') await api(`/reviews/${form.id}`,'PUT',{category:form.category,note:form.note})
  else if(dialog.value==='match') { if(!form.tx || !form.invoice || !form.amount) throw new Error('请选择付款流水、发票并填写正数金额');await api('/matches','POST',{...form}) }
  dialog.value='';await load();ElMessage.success(tr('已保存到本地账务数据库'))
 } catch(e) { formError.value=e instanceof Error ? e.message : String(e);throw e }
}) }
function fileChanged(event: Event) { importFiles.value=Array.from((event.target as HTMLInputElement).files || []);importResults.value=null }
async function importDocuments() { await run(importKind.value==='平台结算'?'正在歸檔 PDF 原件…':'正在解析PDF并核对金额、笔数与余额…',async () => {
 const body=new FormData();body.append('kind',importKind.value);importFiles.value.forEach(f=>body.append('files',f))
 importResults.value=await api<Row>('/import','POST',body);await load();importFiles.value=[]
 if(!importResults.value.errors.length) ElMessage.success(tr(`新增 ${importResults.value.added} 份，重复跳过 ${importResults.value.skipped} 份`))
}) }
async function restoreBackup(event: Event) {
 const input=event.target as HTMLInputElement,file=input.files?.[0];input.value=''
 if(!file)return
 try { await ElMessageBox.confirm(tr('恢復將替換目前帳簿，系統會先保存目前帳簿的備份。接口密鑰保持不變。'),tr('恢復備份'),{type:'warning',confirmButtonText:tr('恢復備份'),cancelButtonText:tr('取消')}) } catch { return }
 await run('正在校驗並恢復備份…',async()=>{
  const body=new FormData();body.append('file',file);body.append('replace_existing','true')
  const result=await api<Row>('/restore','POST',body)
  const settings=await api<Row>('/settings');Object.assign(ledgerSettings,companySettings(settings))
  await load();settingsOpen.value=false;ElMessage.success(tr(`已恢復 ${result.documents} 份憑證及帳簿設定`))
 })
}
async function saveSettings(value: CompanySettings) {
 await run('正在保存設置…',async()=>{
  if(!value.company.trim())throw new Error('請填寫公司名稱')
  const saved={...value,company:value.company.trim()}
  await api('/settings','PUT',{...saved,auto_match_exact:String(saved.auto_match_exact)})
  Object.assign(ledgerSettings,saved)
  settingsOpen.value=false
  await load()
  ElMessage.success(tr('設置已保存'))
 })
}
function download(url:string) { const a=document.createElement('a');a.href=url;a.download='';a.click() }
async function exportPackage() { await run('正在校验凭证并生成Excel与ZIP资料包…',async () => {
 if(!exportMonths.value?.[0] || !exportMonths.value?.[1]) throw new Error('請選擇匯出月份')
 latestDownload.value=await api<Row>('/export','POST',{start:exportMonths.value[0],end:exportMonths.value[1],company:company.value,allow_incomplete:allowIncomplete.value, platform_pdf_shas:exportPlatformPDFs.value});download(latestDownload.value.url);ElMessage.success(tr('资料包已生成，正在下载'))
}) }
async function backup() { await run('正在备份数据库与全部凭证…',async () => { const result=await api<Row>('/backup','POST',{});download(result.url);ElMessage.success(tr('完整备份已生成')) }) }
function statusType(value: unknown): 'success' | 'warning' | 'danger' | 'info' { const s=String(value || ''); if(s.includes('失败')||s.includes('超额')||s.includes('失效')||s.includes('禁止'))return 'danger';if(s.includes('待')||s.includes('缺')||s.includes('不符')||s.includes('部分'))return 'warning';if(s.includes('通过')||s.includes('有效')||s==='已核验'||s==='正式'||s==='已匹配'||s==='無需發票')return 'success';return 'info' }
function rowClassName({row}: {row:Row}) { return row.ok===false || row.duplicate ? 'warning-row' : '' }
function setInvoice(id:string) { const i=invMap.value.get(id); if(i) {form.direction=i.kind==='销售发票'?'收款':'付款';form.ccy=i.ccy || 'HKD';form.amount=i.amount} }
</script>

<template>
 <el-config-provider :locale="uiLocale" size="default">
 <el-container class="app-shell">
  <el-aside width="228px" class="sidebar">
   <div class="brand"><div class="brand-mark"><el-icon :size="25"><Wallet/></el-icon></div><div><strong>{{ tr("港賬通") }}</strong><small>HARBOUR LEDGER</small></div></div>
   <el-menu :default-active="active" @select="(v:string) => active=v as View">
    <el-menu-item v-for="v in visibleViews" :key="v.key" :index="v.key" :aria-label="tr(v.title)"><el-icon><component :is="icons[v.key]"/></el-icon><span>{{ tr(v.title) }}</span><span v-if="v.key==='needs' && missingDocumentCount" class="nav-count">{{ tr(missingDocumentCount) }}</span></el-menu-item>
    <el-sub-menu index="details"><template #title><el-icon><Collection/></el-icon><span>{{tr('記錄與文件')}}</span></template><el-menu-item v-for="v in detailViews" :key="v.key" :index="v.key"><el-icon><component :is="icons[v.key]"/></el-icon><span>{{tr(v.title)}}</span></el-menu-item></el-sub-menu>
   </el-menu>
   <div class="sidebar-bottom"><el-button :icon="Setting" text @click="settingsOpen=true">{{ tr("設置") }}</el-button></div>
 </el-aside>
  <el-container class="main-shell">
   <el-header class="topbar"><span class="company-name">{{ company || tr('尚未設定公司') }}</span><div class="topbar-actions"><el-select v-model="language" style="width:130px" aria-label="Language"><el-option value="en" label="English"/><el-option value="zh-Hant" label="繁體中文"/><el-option value="zh-Hans" label="简体中文"/></el-select><el-button :icon="Refresh" circle :disabled="busy" :aria-label="tr('刷新賬務')" @click="run('正在刷新…',load)"/></div></el-header>
   <el-main>
    <input ref="restoreInput" type="file" accept=".zip" hidden :disabled="busy" @change="restoreBackup"/>
    <el-alert v-if="!data.docs.length" type="info" :closable="false" class="dialog-notice"><template #title>{{tr('開始使用空白帳簿')}}</template><p>{{tr('先設定公司資料，再導入憑證；或選擇備份ZIP恢復完整帳簿。')}}</p><el-button :disabled="busy" @click="settingsOpen=true">{{tr('設定公司')}}</el-button><el-button :disabled="busy" @click="restoreInput?.click()">{{tr('恢復備份')}}</el-button></el-alert>

    <div class="page-heading"><div><h1>{{ tr(selectedView.title) }}</h1></div><div class="heading-actions"><el-button :icon="Upload" :disabled="busy" @click="importKind=active==='docs'?'平台结算':['sales','invoices','unclassified','needs'].includes(active)?'自动发票':'银行';importOpen=true">{{ tr("導入憑證") }}</el-button><el-button type="primary" :icon="Download" :disabled="busy||!data.checks.length" @click="exportOpen=true">{{ tr("導出整理資料包") }}</el-button></div></div>
    <el-alert v-if="error" :title="error" type="error" show-icon closable @close="error=''" class="page-alert"/>
    <div v-if="busy" class="busy-line"><el-icon class="is-loading"><Refresh/></el-icon>{{ tr(loadingText) }}</div>
    <div v-if="(data.docs.length || active!=='overview')" class="period-bar"><div class="period-label">{{ tr("查看月份") }}</div><el-select v-model="periodMode" style="width:140px"><el-option v-for="m in ['全部月份','月度','季度','自然年','自定义']" :key="m" :label="tr(m)" :value="m"/></el-select><template v-if="!['全部月份','自定义'].includes(periodMode)"><el-input-number v-model="year" :min="2000" :max="2100" controls-position="right" style="width:125px"/><el-select v-if="periodMode==='月度'" v-model="month" style="width:100px"><el-option v-for="m in 12" :key="m" :value="m" :label="language==='en'?String(m):`${m}月`"/></el-select><el-select v-if="periodMode==='季度'" v-model="quarter" style="width:100px"><el-option v-for="q in 4" :key="q" :value="q" :label="language==='en'?`Q${q}`:`第${q}季度`"/></el-select></template><el-date-picker v-model="period" type="monthrange" value-format="YYYY-MM" :format="language==='en'?'YYYY-MM':'YYYY年MM月'" :range-separator="tr('至')" :start-placeholder="tr('开始月份')" :end-placeholder="tr('结束月份')" :clearable="false" :disabled="periodMode!=='自定义'"/><span class="period-note">{{ tr(transactions.length) }} {{ tr("筆流水 ·") }} {{ tr(checks.length) }} {{ tr("組賬戶對賬") }}</span></div>
    <template v-if="active==='overview'">
     <div class="workflow-strip"><button :disabled="busy" @click="importKind='银行';importOpen=true"><b>1</b><span>{{tr('導入月結單')}}</span></button><button :disabled="busy" @click="importKind='自动发票';importOpen=true"><b>2</b><span>{{tr('導入發票／收據')}}</span></button><button @click="showGaps('全部')"><b>3</b><span>{{tr('檢查匹配與缺票')}}</span></button><button :disabled="busy||!data.checks.length" @click="exportOpen=true"><b>4</b><span>{{tr('匯出會計資料')}}</span></button></div>
     <div v-if="data.docs.length" class="metrics"><button class="metric" @click="active='tx'"><span>{{ tr("銀行流水") }}</span><strong>{{ tr(transactions.length) }}<small>{{ tr("筆") }}</small></strong><div>{{ tr("按交易所屬月份統計") }} <el-icon><List/></el-icon></div></button><button class="metric amber" @click="showGaps('全部','缺票')"><span>{{ tr("未找到憑證") }}</span><strong>{{ tr(missingDocumentCount) }}<small>{{ tr("筆") }}</small></strong><div>{{ tr("待處理的資料缺口") }} <el-icon><Warning/></el-icon></div></button><button class="metric" @click="active='invoices'"><span>{{ tr("採購／銷售發票") }}</span><strong>{{ tr(invoices.length + data.sales.filter(i=>!i.date||inPeriod(i.date.slice(0,7))).length) }}<small>{{ tr("份") }}</small></strong><div>{{ tr("查看票據與流水匹配") }} <el-icon><Document/></el-icon></div></button><button class="metric green" @click="active='checks'"><span>{{ tr("銀行對賬") }}</span><strong>{{ tr(checks.filter(c=>c.ok).length) }}<small>/ {{ tr(checks.length) }}</small></strong><div>{{ tr("金額、筆數與余額核對") }} <el-icon><CircleCheck/></el-icon></div></button></div>
     <section v-if="financeStats.length" class="panel finance-panel"><div class="panel-heading"><div><h2>{{tr('財務統計')}}</h2><p class="finance-period">{{period?.[0]}} — {{period?.[1]}}</p></div><el-tag effect="plain">{{tr('按銀行實際收支')}}</el-tag></div><div class="finance-grid"><article v-for="f in financeStats" :key="f.ccy" class="finance-currency"><div class="finance-currency-heading"><strong>{{f.ccy}}</strong><span>{{f.count}} {{tr('筆流水')}}</span></div><div class="finance-values"><div><span>{{tr('銀行入賬')}}</span><strong class="income">{{formatMoney(f.incoming/100)}}</strong></div><div><span>{{tr('銀行支出')}}</span><strong>{{formatMoney(f.outgoing/100)}}</strong></div><div><span>{{tr('收支差額')}}</span><strong :class="{'income':f.incoming>=f.outgoing}">{{formatMoney((f.incoming-f.outgoing)/100)}}</strong></div></div><div class="finance-operating"><span>{{tr('扣除內部轉賬及董事往來')}}</span><div>{{tr('收款')}} <b>{{formatMoney(f.operatingIn/100)}}</b><span>·</span>{{tr('付款')}} <b>{{formatMoney(f.operatingOut/100)}}</b></div></div></article></div><p class="footnote">{{tr('分幣種統計；收支差額不等同利潤。')}}</p></section>
     <section v-if="directorTransactions.length" class="panel finance-panel director-panel"><div class="panel-heading"><h2>{{tr('董事往來')}}</h2><div class="director-actions"><span>{{directorTransactions.length}} {{tr('筆流水')}}</span><el-button :disabled="busy||!directorTransactions.length" @click="newDirectorDocument">{{tr('生成借款／還款證明')}}</el-button></div></div><div v-if="directorStats.length" class="finance-grid"><article v-for="f in directorStats" :key="f.ccy" class="finance-currency"><div class="finance-currency-heading"><strong>{{f.ccy}}</strong><span>{{f.count}} {{tr('筆流水')}}</span></div><div class="finance-values"><div><span>{{tr('董事轉入公司')}}</span><strong class="income">{{formatMoney(f.incoming/100)}}</strong></div><div><span>{{tr('公司付予董事')}}</span><strong>{{formatMoney(f.outgoing/100)}}</strong></div><div><span>{{tr('淨轉入')}}</span><strong>{{formatMoney((f.incoming-f.outgoing)/100)}}</strong></div></div></article></div><el-empty v-else :description="tr('當前周期無董事往來')" :image-size="60"/><el-collapse v-if="directorTransactions.length" class="director-details"><el-collapse-item :title="tr('查看往來明細')" name="director"><el-table :data="directorTransactions" stripe><el-table-column prop="date" :label="tr('日期')" width="120"/><el-table-column prop="party" :label="tr('交易對象')" min-width="180"/><el-table-column prop="ccy" :label="tr('幣種')" width="80"/><el-table-column :label="tr('董事轉入公司')" min-width="150" align="right"><template #default="{row}">{{formatMoney(row.dep||0)}}</template></el-table-column><el-table-column :label="tr('公司付予董事')" min-width="150" align="right"><template #default="{row}">{{formatMoney(row.wd||0)}}</template></el-table-column><el-table-column :label="tr('操作')" width="100"><template #default="{row}"><el-button link type="primary" @click="openPreview(row.document_sha)">{{tr('原始PDF')}}</el-button></template></el-table-column></el-table></el-collapse-item></el-collapse><p class="footnote">{{tr('淨轉入為所選周期轉入減轉出，不代表董事往來期末餘額。')}}</p></section>
     <section v-if="transactions.length" class="panel inspection-overview"><div class="panel-heading"><h2>{{tr('憑證核查')}}</h2><el-button link type="primary" @click="showGaps()">{{tr('查看全部')}}</el-button></div><div class="inspection-summary"><button v-for="g in inspectionGroups" :key="g.key" :class="g.key" @click="active='needs';gapFilter=g.key"><span>{{tr(g.label)}}</span><strong>{{g.count}}</strong></button></div></section>
     <div v-if="data.checks.length" class="coverage-grid"><section class="panel"><div class="panel-heading"><h2>{{ tr("月結單覆蓋") }}</h2><el-tag :type="missingMonths.length?'warning':'success'">{{ tr(missingMonths.length?'存在缺月':'周期完整') }}</el-tag></div><div class="coverage-number">{{ tr(new Set(checks.map(c=>c.period)).size) }} <small>{{ tr("個月已導入") }}</small></div><p class="coverage-copy">{{ tr(missingMonths.length ? `尚缺 ${missingMonths.length} 个月的月结单` : '所选周期的月份均有月结单') }}</p><div class="month-tags"><el-tag v-for="m in missingMonths.slice(0,8)" :key="m" type="warning" effect="plain">{{ tr(m) }}</el-tag><span v-if="missingMonths.length>8">…</span></div><el-button :icon="Upload" link type="primary" @click="importKind='银行';importOpen=true">{{ tr("導入銀行月結單") }}</el-button></section></div>


    </template>
    <section v-else class="panel table-panel">
     <template v-if="active==='docs'"><el-alert v-if="organizeResult" :title="`规范命名 ${organizeResult.renamed} 份，清理重复副本 ${organizeResult.removed} 份；另有 ${organizeResult.unknown_files.length} 份无引用且内容不同的文件保留。`" type="success" :closable="false"/><el-link v-if="organizeResult?.backup_url" :href="organizeResult.backup_url" type="primary">{{ tr("下載整理前完整備份") }}</el-link></template>
     <div v-if="active==='needs'" class="inspection-filters"><div class="inspection-summary"><button v-for="g in inspectionGroups" :key="g.key" :class="[g.key,{selected:gapFilter===g.key}]" :aria-pressed="gapFilter===g.key" @click="gapFilter=gapFilter===g.key?'全部':g.key"><span>{{tr(g.label)}}</span><strong>{{g.count}}</strong></button></div><div class="inspection-heading"><strong>{{tr(gapFilter==='全部'?'全部流水':inspectionLabels[gapFilter as InspectionBucket])}}</strong><el-button v-if="gapFilter!=='全部'" link @click="gapFilter='全部'">{{tr('查看全部')}}</el-button><el-button v-if="gapFilter==='director'" :disabled="busy" @click="newDirectorDocument">{{tr('生成借款／還款證明')}}</el-button></div></div>
     <div class="table-toolbar"><div class="search-group"><el-input v-model="query" :prefix-icon="Search" clearable :placeholder="tr('搜索編號、對象、摘要或文件名')" style="width:310px"/><el-select v-if="!['docs','needs'].includes(active)" v-model="statusFilter" style="width:120px"><el-option v-for="s in ['全部','待处理','已完成']" :key="s" :value="s" :label="tr(s)"/></el-select><span class="row-total">{{ tr(filtered.length) }} {{ tr("條記錄") }}</span></div><div><el-button v-if="active==='docs'" type="primary" :disabled="busy" @click="organizeDocuments">{{ tr("按內容整理 / 清理重復") }}</el-button><el-button v-if="active==='matches'" :disabled="busy" :icon="Refresh" @click="run('正在刷新收付款关联候选…',async()=>{await api('/matches/refresh','POST',{});await load()})">{{ tr("自動匹配") }}</el-button><el-button v-if="active==='matches'" type="primary" :icon="Plus" :disabled="busy" @click="newMatch">{{ tr("新增關聯") }}</el-button></div></div>
     <el-table :data="pageRows" stripe :height="active==='needs'?'max(300px, calc(100vh - 450px))':'min(620px, calc(100vh - 355px))'" @row-dblclick="edit" empty-text="當前周期暫無記錄，可調整周期或導入憑證" :row-class-name="rowClassName" @sort-change="sortChanged">
      <el-table-column v-if="active==='tx'||(active==='needs'&&rows.some(r=>linkedInvoices(r).length))" :label="tr('對應票據')" :width="active==='needs'?180:250"><template #default="{row}"><div v-if="linkedInvoices(row).length" class="linked-invoices"><div v-for="i in linkedInvoices(row)" :key="i.id"><el-button link type="primary" @click.stop="openPreview(i.document_sha)">{{i.number}} · PDF</el-button><span>{{i.customer&&i.kind==='销售发票'?i.customer:i.vendor}}</span><small>{{i.ccy}} {{formatMoney(i.amount)}}</small><small>{{tr(invoiceLinkNote(row,i))}}</small></div></div><span v-else class="muted">—</span></template></el-table-column>
      <el-table-column v-for="c in currentColumns" :key="c.key" :prop="c.key" :label="tr(c.label)" :min-width="c.width || 140" :align="c.money?'right':'left'" show-overflow-tooltip sortable="custom"><template #default="{row}"><el-select v-if="c.key==='display_category'" :model-value="row.manual_category || row.category" filterable allow-create default-first-option :disabled="busy" @change="(v:string)=>setCategory(row,v)"><el-option v-if="!categoryOptions.includes(row.manual_category||row.category)" :value="row.manual_category||row.category" :label="tr(row.manual_category||row.category)"/><el-option v-for="v in categoryOptions" :key="v" :value="v" :label="tr(v)"/></el-select><div v-else-if="c.key==='inspection_result'" class="inspection-result"><el-tag :type="statusType(row.inspection_result)">{{tr(row.inspection_result)}}</el-tag><small v-if="row.inspection_reason">{{tr(row.inspection_reason)}}</small></div><span v-else-if="c.money" class="money-cell"><small v-if="active==='needs'" class="flow-label">{{tr(row.wd>0?'付款':'收款')}}</small>{{ formatMoney(row[c.key]) }}</span><el-tag v-else-if="['state','verification','status','validity','check_status'].includes(c.key)" :type="statusType(row[c.key])" effect="light">{{ ['state','verification','status','validity','check_status','direction_basis','method','settlement_state','age_bucket','display_category','need','documents','kind','pdf_type_label','gap_type','fx_status','document_type_label','review_type','review_reason','inspection_result','inspection_reason'].includes(c.key)?tr(row[c.key]||'—'):(row[c.key]||'—') }}</el-tag><span v-else>{{ ['state','verification','status','validity','check_status','direction_basis','method','settlement_state','age_bucket','display_category','need','documents','kind','pdf_type_label','gap_type','fx_status','document_type_label','review_type','review_reason','inspection_result','inspection_reason'].includes(c.key)?tr(row[c.key]||'—'):(row[c.key]||'—') }}</span></template></el-table-column>
      <el-table-column :label="tr('操作')" fixed="right"  :width="active==='needs'?(gapFilter==='missing'||gapFilter==='pending'?220:130):310"><template #default="{row}"><el-button v-if="['tx','invoices','sales','unclassified','matches'].includes(active)" link type="primary" :disabled="busy" @click="edit(row)">{{ tr(['invoices','sales'].includes(active)?'查看／修改':active==='matches'?'复核':'分类') }}</el-button><template v-if="(active==='tx'||(active==='needs'&&['missing','pending'].includes(row.inspection_bucket))) && (row.wd > 0 || row.dep > 0)"><el-button link type="primary" :disabled="busy" @click="matchPayment(row)">{{ tr("關聯發票") }}</el-button></template><el-button v-if="['needs','tx'].includes(active) && row.dep>0 && inspectionBucket(row)==='pending'" link type="primary" @click="newPlatform(row)">{{tr('核對收款差額')}}</el-button><el-button v-if="row.document_sha || row.sha" link type="primary" @click="openPreview(row.document_sha || row.sha)">{{ tr(['needs','tx'].includes(active)?'月結單':'原始PDF') }}</el-button></template></el-table-column>
     </el-table><div class="table-footer"><span>{{ tr("雙擊記錄可編輯或查看原件") }}</span><el-pagination v-model:current-page="page" v-model:page-size="pageSize" :page-sizes="[30,60,100]" layout="total, sizes, prev, pager, next" :total="filtered.length"/></div>
    </section>
    
   </el-main>
  </el-container>
 </el-container>

 <el-dialog v-model="importOpen" :title="tr('導入憑證')" width="650px" :close-on-click-modal="!busy" :show-close="!busy">
  <el-radio-group v-model="importKind" :disabled="busy"><el-radio-button value="银行">{{ tr("銀行月結單") }}</el-radio-button><el-radio-button value="自动发票">{{ tr("發票（自動辨識銷售／採購）") }}</el-radio-button><el-radio-button value="银行通知">{{tr("銀行收款通知")}}</el-radio-button><el-radio-button value="平台结算">{{ tr("平臺報告 PDF（Stripe 等）") }}</el-radio-button></el-radio-group>
  <el-alert :title="importKind==='银行通知'?tr('本地解析收款通知，核對付款方、原款、費用及淨入帳；不冒充銷售發票。'):importKind==='平台结算'?tr('Stripe 餘額摘要等報告按原件歸檔，可在憑證文件查看，匯出時按需選入資料包。'):importKind==='银行'?'银行月结单仅使用本地Go解析，不上传腾讯云；支持汇丰Business Direct文字型PDF，金额、笔数与余额须核对通过。':(ocrStatus.configured?'自动检查PDF类型：文字型本地解析；仅扫描型或含扫描页发票调用腾讯云OCR，结果待核验。':'未配置腾讯云：文字PDF本地提取，扫描件保留原件并进入人工核验。')" type="info" :closable="false" show-icon class="dialog-notice"/>
  <label class="file-picker"><el-icon :size="32"><Upload/></el-icon><strong>{{ tr("選擇本地 PDF 文件") }}</strong><span>{{ tr("支持多選，每批最多50份 / 100MB") }}</span><input type="file" accept=".pdf,application/pdf" multiple :disabled="busy" @change="fileChanged"></label>
  <div class="file-list"><div v-for="f in importFiles" :key="f.name+f.size">{{ f.name }}<span>{{ tr((f.size/1024/1024).toFixed(2)) }} MB</span></div></div>
  <el-table v-if="importResults?.files?.length" :data="importResults.files" max-height="260"><el-table-column prop="archive_name" :label="tr('按內容自動命名')"/><el-table-column prop="status" :label="tr('處理結果')"/></el-table>
  <template v-if="importResults"><el-alert :title="`新增 ${importResults.added} 份，重复跳过 ${importResults.skipped} 份，失败 ${importResults.errors.length} 份`" :type="importResults.errors.length?'warning':'success'" :closable="false"/><div v-for="w in importResults.warnings || []" :key="w.name" class="import-error"><strong>{{ w.name }}</strong><p>{{ tr(w.warning) }}</p></div><div v-for="e in importResults.errors" :key="e.name" class="import-error"><strong>{{ e.name }}</strong><p>{{ tr(e.error) }}</p></div></template>
  <template #footer><el-button :disabled="busy" @click="importOpen=false">{{ tr("關閉") }}</el-button><el-button type="primary" :loading="busy" :disabled="!importFiles.length" @click="importDocuments">{{ tr("開始導入") }}</el-button></template>
 </el-dialog>

 <el-dialog :model-value="!!dialog" :title="tr(dialogTitle)" width="760px" :close-on-click-modal="false" :show-close="!busy" @close="dialog=''">
  <el-alert v-if="formError" :title="tr(formError)" type="error" :closable="false" class="dialog-notice"/>
  <el-form label-position="top" class="edit-form" :disabled="busy">
   <template v-if="dialog==='platform'"><el-form-item :label="tr('收款差額')"><el-select v-model="form.settlement_kind"><el-option value="跨境匯款" :label="tr('跨境匯款')"/><el-option value="平台结算" :label="tr('平臺結算')"/></el-select></el-form-item><el-form-item :label="tr('對應發票')"><el-select v-model="form.invoice" filterable clearable @change="chooseSettlementInvoice"><el-option v-for="i in data.sales" :key="i.id" :value="i.id" :label="`${i.number} | ${i.customer} | ${i.ccy} ${formatMoney(i.amount)}`"/></el-select></el-form-item><el-alert :title="tr('銷售總額－手續費－退款＋調整＝銀行凈到賬。同幣種、無退款和調整且人工核驗的報告，可用于銷售發票按總額分配；含退款的結算須另核對貸項，不能直接核銷。')" type="info" :closable="false"/><el-form-item :label="tr('銀行實際收款')"><el-select v-model="form.tx" filterable><el-option v-for="t in data.tx.filter(t=>t.dep>0)" :key="t.id" :value="t.id" :label="`${t.date} ${t.ccy} ${formatMoney(t.dep)} ${t.id}`"/></el-select></el-form-item><el-form-item :label="tr('平臺結算編號')"><el-input v-model="form.payout_id"/></el-form-item><div class="form-grid"><el-form-item :label="tr('幣種')"><el-select v-model="form.ccy"><el-option value="HKD"/><el-option value="USD"/></el-select></el-form-item><el-form-item v-for="k in ['gross','fees','refunds','adjustments']" :key="k" :label="tr(({gross:'销售总额',fees:'手续费',refunds:'退款',adjustments:'调整'} as Record<string,string>)[k])"><el-input-number v-model="form[k]" :precision="2"/></el-form-item></div><el-form-item :label="tr('報告原件（先從導入憑證上傳平臺結算PDF）')"><el-select v-model="form.evidence_sha" filterable><el-option v-for="d in data.docs" :key="d.sha" :value="d.sha" :label="d.name"/></el-select></el-form-item><el-form-item :label="tr('狀態')"><el-select v-model="form.status"><el-option value="待复核"/><el-option value="已核验"/></el-select></el-form-item><el-form-item :label="tr('復核人')"><el-input v-model="form.reviewer"/></el-form-item><el-form-item :label="tr('差額依據（匯款通知／扣費資料）')"><el-input v-model="form.fee_basis" type="textarea"/></el-form-item><p>{{tr('差額僅為參考，核對手續費依據後才能確認；不修改發票原額。')}} {{tr('計算淨額')}}: {{form.ccy}} {{formatMoney(settlementNet)}}</p></template>
   <template v-if="dialog==='review'"><div class="transaction-summary">{{ tr(form.date) }} · {{ tr(form.ccy) }} {{ tr(formatMoney(form.wd || form.dep)) }}<p>{{ form.details }}</p></div><el-form-item :label="tr('確認分類')"><el-select v-model="form.category" filterable allow-create default-first-option><el-option v-for="c in categoryOptions" :key="c" :value="c" :label="tr(c)"/></el-select></el-form-item><el-form-item :label="tr('復核說明')"><el-input v-model="form.note" type="textarea" :rows="4" maxlength="4000"/></el-form-item></template>
   <template v-if="dialog==='invoice'"><el-form-item :label="tr('憑證類型')"><el-select v-model="form.document_type"><el-option value="invoice" :label="tr('發票')"/><el-option value="payment_receipt" :label="tr('付款收據')"/></el-select></el-form-item><el-alert :title="tr('收據或發票均可支持同一筆交易，不重複計算。')" type="info" :closable="false"/><el-form-item :label="tr('確認發票方向')" required><el-select v-model="form.direction"><el-option value="发票" :label="tr('採購發票')"/><el-option value="销售发票" :label="tr('銷售發票')"/></el-select></el-form-item><el-alert v-if="form.direction_basis" :title="form.direction_basis" type="info" :closable="false"/><el-alert v-if="form.duplicate" :title="`与 ${form.duplicate} 重复，禁止重复核销`" type="warning" :closable="false"/><div class="form-grid"><el-form-item :label="tr('發票日期')" required><el-date-picker v-model="form.date" value-format="YYYY-MM-DD" :placeholder="tr('選擇日期')"/></el-form-item><el-form-item :label="tr('發票編號')" required><el-input v-model="form.number"/></el-form-item></div><el-form-item v-if="form.direction==='销售发票'" :label="tr('客戶')" required><el-input v-model="form.customer"/></el-form-item><el-form-item v-else :label="tr('供應商')" required><el-input v-model="form.vendor"/></el-form-item><div class="form-grid"><el-form-item :label="tr('幣種')" required><el-select v-model="form.ccy"><el-option label="HKD" value="HKD"/><el-option label="USD" value="USD"/></el-select></el-form-item><el-form-item :label="tr('金額')" required><el-input-number v-model="form.amount" :precision="2" :min="0.01" :max="999999999999"/></el-form-item></div><el-form-item v-if="form.direction!=='销售发票'" :label="tr('收款賬號（多個賬號以逗號分隔）')"><el-input v-model="form.accounts_text"/></el-form-item><el-form-item :label="tr('服務期間（按發票／訂單填寫）')"><el-input v-model="form.service_period" :placeholder="tr('例如 2026-03-01 至 2026-03-31')"/></el-form-item><el-form-item :label="tr('備註')"><el-input v-model="form.note" type="textarea" :rows="3"/></el-form-item><div class="form-grid"><el-form-item v-if="form.direction==='销售发票'" :label="tr('資料確認人（選填）')"><el-input v-model="form.reviewer"/></el-form-item><el-form-item :label="tr('約定到期日（未知留空）')"><el-date-picker v-model="form.due_date" value-format="YYYY-MM-DD"/></el-form-item></div><el-checkbox v-if="form.direction==='销售发票'" v-model="form.verified">{{ tr("已打開原件核對交易對象、日期、編號、金額及相關賬號") }}</el-checkbox></template>
   <template v-if="dialog==='match'"><el-form-item :label="tr('方向')"><el-select v-model="form.direction" @change="form.tx='';form.invoice='';form.platform_id=''"><el-option value="付款"/><el-option value="收款"/></el-select></el-form-item><el-form-item :label="tr('實際銀行收付款流水')" required><el-select v-model="form.tx" filterable :placeholder="tr('搜索日期或流水編號')"><el-option v-for="t in matchTransactions" :key="t.id" :value="t.id" :label="`${t.date} | 銀行 ${t.ccy} ${formatMoney(t.wd || t.dep)} | 原幣 ${t.orig_ccy ? t.orig_ccy+' '+formatMoney(t.orig_amount) : '未列示'} | ${t.id}`"/></el-select></el-form-item><el-form-item v-if="form.direction==='收款'" :label="tr('平臺結算（Stripe等凈結算時選擇）')"><el-select v-model="form.platform_id" clearable><el-option v-for="p in data.platforms.filter(p=>p.tx===form.tx)" :key="p.id" :value="p.id" :label="`${p.payout_id} ${p.status}`"/></el-select></el-form-item><el-alert v-if="form.tx" :title="paymentSummary" type="info" :closable="false"/><el-form-item :label="tr('對應發票')" required><el-select v-model="form.invoice" filterable @change="setInvoice"><el-option v-for="i in matchableInvoices" :key="i.id" :value="i.id" :label="`${i.customer || i.vendor} | ${i.number} | ${i.ccy} ${formatMoney(i.amount)}`"/></el-select></el-form-item><div class="form-grid"><el-form-item :label="tr('分配幣種')" required><el-select v-model="form.ccy"><el-option label="HKD" value="HKD"/><el-option label="USD" value="USD"/></el-select></el-form-item><el-form-item :label="tr('分配金額')" required><el-input-number v-model="form.amount" :precision="2" :min="0.01" :max="999999999999"/></el-form-item><el-form-item :label="tr('確認狀態')"><el-select v-model="form.status"><el-option v-for="s in ['待确认','已确认','不匹配']" :key="s" :label="tr(s)" :value="s"/></el-select></el-form-item><el-form-item :label="tr('復核人（確認時必填）')"><el-input v-model="form.reviewer"/></el-form-item></div><el-form-item :label="tr('復核說明（賬號不符時必填）')"><el-input v-model="form.note" type="textarea"/></el-form-item><el-form-item :label="tr('補充說明（選填）')"><el-input v-model="form.evidence" type="textarea"/></el-form-item><el-alert :title="tr('同幣同額且唯一對應的採購付款自動匹配；收款差額另行核對。')" type="info" :closable="false"/></template>
  </el-form><template #footer><el-button v-if="form.document_sha" @click="openPreview(form.document_sha)">{{ tr("查看原始PDF") }}</el-button><el-button :disabled="busy" @click="dialog=''">{{ tr("取消") }}</el-button><el-button type="primary" :loading="busy" @click="saveForm">{{ tr(dialog==='generate'?'生成PDF并归档':'保存并校验') }}</el-button></template>
 </el-dialog>

 <el-dialog v-model="exportOpen" :title="tr('匯出資料')" width="560px" :show-close="!busy" :close-on-click-modal="!busy"><el-form label-position="top" class="edit-form"><el-form-item :label="tr('匯出月份')"><el-date-picker v-model="exportMonths" type="monthrange" value-format="YYYY-MM" format="YYYY-MM" :range-separator="tr('至')" :clearable="false"/></el-form-item><el-form-item v-if="platformPDFs.length" :label="tr('附加平臺報告 PDF（選填）')"><el-select v-model="exportPlatformPDFs" multiple filterable clearable style="width:100%"><el-option v-for="doc in platformPDFs" :key="doc.sha" :label="doc.name" :value="doc.sha"/></el-select></el-form-item></el-form><el-alert v-if="exportChecks.some(c=>!c.ok)" :title="tr('所選月份有銀行對賬失敗記錄，請先處理。')" type="error" :closable="false"/><el-alert v-else-if="exportMissingMonths.length" :title="`${tr('缺少月結單')}：${exportMissingMonths.join('、')}`" type="warning" :closable="false"/><el-checkbox v-if="exportMissingMonths.length" v-model="allowIncomplete" class="dialog-notice">{{tr('仍匯出，並列明缺少月份')}}</el-checkbox><p class="footnote">{{tr('XLSX台賬及PDF原件，含憑證清單和匹配結果。')}}</p><el-link v-if="latestDownload" :href="latestDownload.url" type="primary">{{tr('下載資料包')}}</el-link><template #footer><el-button :disabled="busy" @click="exportOpen=false">{{tr('關閉')}}</el-button><el-button type="primary" :loading="busy" :disabled="!exportChecks.length||exportChecks.some(c=>!c.ok)||!!exportMissingMonths.length&&!allowIncomplete" @click="exportPackage">{{tr('匯出ZIP')}}</el-button></template></el-dialog>
 <LedgerSettings v-model="settingsOpen" :value="ledgerSettings" :accounts="accounts" :busy="busy" :has-documents="!!data.docs.length"
  @save="saveSettings" @backup="backup" @restore="restoreInput?.click()" @connection-saved="refreshConnectionStatus"
  @rebuild="settingsOpen=false;rebuildOpen=true" @edit-account="editAccount"/>

 <el-dialog v-model="rebuildOpen" :title="tr('重新梳理全部賬務')" width="900px" :close-on-click-modal="false" :show-close="!busy">
  <el-alert :title="tr('處理全部已導入憑證，不受當前賬務周期限制。自動備份、校驗原件、重新解析銀行流水、補全發票字段、識別利息與董事往來、自動關聯發票；保留所有未匹配發票，符合設定規則的採購關聯自動確認，其餘候選待核對，并整理文件名稱和同內容副本。')" type="info" :closable="false"/>
  <p>{{ tr("銀行月結單始終只用本地解析。人工核驗字段、分類說明和已確認關聯保留；自制憑證檢查原件及關聯。無法識別的文件保留并列入結果。") }}</p>
  <el-checkbox v-model="rebuildCloud" :disabled="busy || !ocrStatus.configured">{{ tr("使用騰訊云識別未核驗掃描型發票（文字型始終本地解析，掃描識別可能產生費用）") }}</el-checkbox>
  <p v-if="!rebuildCloud" class="footnote">{{ tr("本次全部使用本地解析，不調用付費OCR。掃描件無法識別時仍需人工核驗。") }}</p>
  <el-progress v-if="busy && ['queued','running'].includes(rebuildJob.status)" :percentage="rebuildJob.total ? Math.floor(rebuildJob.done/rebuildJob.total*100) : 0"/>
  <p v-if="busy">{{ tr(loadingText) }}</p>
  <el-alert v-if="rebuildJob.status==='failed'" :title="rebuildJob.error" type="error" :closable="false"/>
  <template v-if="rebuildResult"><p>{{ tr("處理") }} {{ tr(rebuildResult.total) }} {{ tr("份憑證 ·") }} {{ tr(rebuildResult.transactions || 0) }} {{ tr("筆流水 ·") }} {{ tr(rebuildResult.issues) }} {{ tr("份需檢查 ·") }} {{ tr(rebuildResult.failed_checks || 0) }} {{ tr("組對賬未通過 ·") }} {{ tr(rebuildResult.needs || 0) }} {{ tr("筆待補憑證") }}</p><el-link v-if="rebuildResult.backup_url" :href="rebuildResult.backup_url" type="primary">{{ tr("下載梳理前完整備份") }}</el-link><el-table :data="rebuildResult.files" max-height="350"><el-table-column prop="name" :label="tr('憑證')" min-width="250" show-overflow-tooltip/><el-table-column prop="kind" :label="tr('類型')" width="80"/><el-table-column prop="status" :label="tr('處理結果')" min-width="220"/><el-table-column prop="note" :label="tr('原因 / 提示')" min-width="200" show-overflow-tooltip/></el-table></template>
  <template #footer><el-button :disabled="busy" @click="rebuildOpen=false">{{ tr("關閉") }}</el-button><el-button type="primary" :loading="busy" :disabled="!data.docs.length" @click="rebuildLedger">{{ tr("開始梳理全部") }} {{ tr(data.docs.length) }} {{ tr("份憑證") }}</el-button></template>
 </el-dialog>
 <el-dialog v-model="accountOpen" :title="tr('賬戶基礎信息')" width="680px" :close-on-click-modal="false" :show-close="!busy">
  <el-alert :title="accountForm.first_statement_period ? `首份已导入月结单：${accountForm.first_statement_period}。此月份仅作最早记录参考，不等于实际开户日期。` : '尚无月结单记录。实际开户日期可根据银行开户确认文件填写。'" type="info" :closable="false"/>
  <el-button v-if="accountForm.first_statement_sha" link type="primary" @click="openPreview(accountForm.first_statement_sha)">{{ tr("查看首份月結單") }}</el-button>
  <p v-if="accountForm.subaccounts?.length">{{ tr("账户内类别：") }}{{ tr(accountForm.subaccounts.map((a:Row)=>`${a.account} (${a.ccy})`).join('、')) }}</p>
  <el-alert v-if="accountForm.metadata_conflicts?.length" :title="tr('舊幣種資料存在不同的銀行名稱、日期、狀態或備注；已保留舊記錄，請核對后填寫主賬戶資料。')" type="warning" :closable="false"/>
  <el-form label-position="top" :disabled="busy">
   <div class="form-grid"><el-form-item :label="tr('銀行名稱')" required><el-input v-model="accountForm.bank_name" maxlength="200"/></el-form-item><el-form-item :label="tr('銀行賬號')" required><el-input v-model="accountForm.bank_number" maxlength="200"/></el-form-item><el-form-item :label="tr('主賬戶名稱 / 類型')" required><el-input v-model="accountForm.account" :placeholder="tr('例如 公司往來帳戶')" maxlength="200"/></el-form-item><el-form-item :label="tr('支持幣種')"><el-select v-model="accountForm.currencies" multiple><el-option value="HKD"/><el-option value="USD"/></el-select></el-form-item><el-form-item :label="tr('實際開通日期（銀行確認）')"><el-date-picker v-model="accountForm.opened_on" value-format="YYYY-MM-DD" :placeholder="tr('未知可留空')"/></el-form-item><el-form-item :label="tr('銷戶時間')"><el-date-picker v-model="accountForm.closed_on" value-format="YYYY-MM-DD"/></el-form-item><el-form-item :label="tr('賬戶狀態')"><el-select v-model="accountForm.status"><el-option v-for="s in ['待确认','正常','停用','已销户']" :key="s" :value="s"/></el-select></el-form-item></div>
   <el-form-item :label="tr('備注')"><el-input v-model="accountForm.note" type="textarea" :rows="3" maxlength="2000"/></el-form-item>
  </el-form><template #footer><el-button :disabled="busy" @click="accountOpen=false">{{ tr("取消") }}</el-button><el-button type="primary" :loading="busy" @click="saveAccount">{{ tr("保存賬戶信息") }}</el-button></template>
 </el-dialog>
 <el-dialog v-model="directorDocOpen" :title="tr('董事借款／還款證明')" width="720px" :show-close="!busy" :close-on-click-modal="!busy"><el-form label-position="top" class="edit-form"><el-form-item :label="tr('款項性質')"><el-radio-group v-model="directorDocForm.kind"><el-radio-button value="borrow">{{tr('董事借給公司')}}</el-radio-button><el-radio-button value="repay">{{tr('公司償還董事借款')}}</el-radio-button></el-radio-group></el-form-item><el-form-item :label="tr('選擇流水')"><el-button link type="primary" @click="directorDocForm.tx_ids=directorDocRows.map(t=>String(t.id))">{{tr('全選')}}</el-button><el-select v-model="directorDocForm.tx_ids" multiple filterable :placeholder="tr('按實際性質選擇')"><el-option v-for="t in directorDocRows" :key="t.id" :value="t.id" :label="`${t.date} | ${t.party} | ${t.ccy} ${formatMoney(t.dep||t.wd)}`"/></el-select></el-form-item><el-form-item :label="tr('對應借款／協議參考（選填）')"><el-input v-model="directorDocForm.loan_reference" :maxlength="300"/></el-form-item><el-form-item :label="tr('借款條款／說明（選填）')"><el-input v-model="directorDocForm.terms" type="textarea" :rows="3" :maxlength="1500"/></el-form-item><p class="footnote">{{tr('僅生成未簽署草稿，不改變流水分類。代墊報銷、薪酬或增資請勿選作借款。')}}</p><el-link v-if="directorDocResult" :href="directorDocResult.url" target="_blank" type="primary">{{directorDocResult.name}}</el-link></el-form><template #footer><el-button :disabled="busy" @click="directorDocOpen=false">{{tr('關閉')}}</el-button><el-button type="primary" :loading="busy" :disabled="!directorDocForm.tx_ids.length" @click="generateDirectorDocument">{{tr('生成並預覽PDF')}}</el-button></template></el-dialog>
 <el-dialog v-model="previewOpen" :title="tr('原始PDF憑證')" width="90%" top="3vh" class="pdf-dialog"><iframe v-if="previewOpen" :src="previewBlob || `/api/documents/${previewSha}`" :title="tr('原始PDF預覽')"/><template #footer><el-link :href="previewBlob || `/api/documents/${previewSha}`" target="_blank" rel="noopener" type="primary">{{ tr("在新窗口打開PDF") }}</el-link><el-button @click="previewOpen=false">{{ tr("關閉") }}</el-button></template></el-dialog>
</el-config-provider>
</template>











