<script setup lang="ts">
import { defineAsyncComponent, reactive, ref, watch } from 'vue'
import { t as tr } from './language'
import { companySettings, type CompanySettings } from './settings'
import type { Row } from './types'

const ConnectionSettings = defineAsyncComponent(() => import('./ConnectionSettings.vue'))
const props = defineProps<{
 modelValue: boolean
 value: CompanySettings
 accounts: Row[]
 busy: boolean
 hasDocuments: boolean
}>()
const emit = defineEmits<{
 'update:modelValue': [value: boolean]
 save: [value: CompanySettings]
 backup: []
 restore: []
 rebuild: []
 connectionSaved: []
 editAccount: [value?: Row]
}>()
const tab = ref('company')
const draft = reactive(companySettings())
watch(() => props.modelValue, open => {
 if (open) {
  Object.assign(draft, props.value)
  tab.value = 'company'
 }
}, { immediate: true })
</script>

<template>
 <el-dialog :model-value="modelValue" :title="tr('設置')" width="680px"
  :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy"
  @update:model-value="emit('update:modelValue', $event)">
  <el-tabs v-model="tab">
   <el-tab-pane :label="tr('公司資料')" name="company">
    <el-form label-position="top" :disabled="busy">
     <el-form-item :label="tr('公司名稱')" required><el-input v-model="draft.company" maxlength="200"/></el-form-item>
     <el-collapse>
      <el-collapse-item :title="tr('識別與匹配（選填）')" name="recognition">
       <el-form-item :label="tr('公司名稱別名（選填）')"><el-input v-model="draft.company_aliases" type="textarea" :placeholder="tr('多個正式名稱以分號分隔')" maxlength="500"/></el-form-item>
       <el-form-item :label="tr('董事姓名（選填）')"><el-input v-model="draft.director_names" type="textarea" :placeholder="tr('每行一位；留空不自動識別董事往來')" maxlength="500"/></el-form-item>
       <el-form-item :label="tr('唯一候選自動匹配')"><el-switch v-model="draft.auto_match_exact"/><span class="settings-help">{{tr('預設關閉，候選由使用者核對後確認')}}</span></el-form-item>
      </el-collapse-item>
      <el-collapse-item :title="tr('銀行帳戶')" name="accounts">
       <p class="settings-help">{{tr('導入月結單後自動建立帳戶，可補充銀行名稱和開戶日期。')}}</p>
       <el-table :data="accounts" max-height="220" :empty-text="tr('尚無銀行帳戶')">
        <el-table-column prop="bank_number" :label="tr('賬號')"/>
        <el-table-column prop="first_statement_period" :label="tr('首份月結單')" width="130"/>
        <el-table-column :label="tr('操作')" width="75"><template #default="{row}"><el-button link type="primary" :disabled="busy" @click="emit('editAccount', row)">{{tr('編輯')}}</el-button></template></el-table-column>
       </el-table>
       <el-button :disabled="busy" @click="emit('editAccount')">{{tr('新增帳戶')}}</el-button>
      </el-collapse-item>
     </el-collapse>
    </el-form>
   </el-tab-pane>
   <el-tab-pane :label="tr('備份與恢復')" name="backup">
    <p>{{tr('備份保存完整帳簿、公司設定及原始憑證。')}}</p>
    <div class="settings-actions"><el-button type="primary" :loading="busy" @click="emit('backup')">{{tr('備份帳簿')}}</el-button><el-button :disabled="busy" @click="emit('restore')">{{tr('恢復備份')}}</el-button></div>
    <p class="settings-help">{{tr('恢復前會自動備份目前帳簿；接口密鑰不隨備份轉移。')}}</p>
   </el-tab-pane>
   <el-tab-pane :label="tr('可選功能')" name="connections" lazy>
    <p class="settings-help">{{tr('日常導入、核對和匯出均可離線使用。按需配置以下服務。')}}</p>
    <el-form v-if="tab==='connections'" label-position="top" :disabled="busy"><ConnectionSettings @saved="emit('connectionSaved')"/></el-form>
   </el-tab-pane>
   <el-tab-pane :label="tr('資料維護')" name="maintenance">
    <p>{{tr('需要重新識別已導入憑證時使用。處理前自動備份，保留人工核對結果。')}}</p>
    <el-button :disabled="busy||!hasDocuments" @click="emit('rebuild')">{{tr('重新梳理賬務')}}</el-button>
   </el-tab-pane>
  </el-tabs>
  <template #footer>
   <el-button :disabled="busy" @click="emit('update:modelValue', false)">{{tr('關閉')}}</el-button>
   <el-button v-if="tab==='company'" type="primary" :loading="busy" @click="emit('save', {...draft})">{{tr('保存公司資料')}}</el-button>
  </template>
 </el-dialog>
</template>

<style scoped>
.settings-help{font-size:13px;color:#667e8d;line-height:1.7}.el-switch+.settings-help{margin-left:12px}.settings-actions{display:flex;gap:12px;margin:24px 0}.settings-actions .el-button+.el-button{margin-left:0}.el-tab-pane{min-height:210px;padding:12px 0}.el-collapse .el-form-item{margin:16px 0}.el-table+.el-button{margin-top:12px}
</style>
