<script setup lang="ts">
import {onMounted,reactive,ref} from 'vue'
import {ElMessage} from 'element-plus'
import {api} from './api'
import type {Row} from './types'
const emit=defineEmits<{saved:[]}>()
const state=ref<Row>({configured:{},locked:{},editable:true}),values=reactive<Row>({}),clear=ref<string[]>([]),busy=ref(false),error=ref('')
const ocrFields=[{key:'TENCENTCLOUD_SECRET_ID',label:'Secret ID'},{key:'TENCENTCLOUD_SECRET_KEY',label:'Secret Key'},{key:'TENCENTCLOUD_TOKEN',label:'臨時 Token（選填）'}]
async function load(){try{state.value=await api<Row>('/config');Object.keys(values).forEach(k=>delete values[k]);Object.assign(values,state.value.values);clear.value=[]}catch(e){error.value=e instanceof Error?e.message:String(e)}}
async function save(){busy.value=true;error.value='';try{const input:Row={};for(const [key,value] of Object.entries(values)){if(!state.value.locked[key])input[key]=String(value??'')}await api('/config','PUT',{values:input,clear:clear.value});await load();emit('saved');ElMessage.success('接口配置已保存並生效')}catch(e){error.value=e instanceof Error?e.message:String(e)}finally{busy.value=false}}
function toggleClear(key:string){if(clear.value.includes(key))clear.value=clear.value.filter(k=>k!==key);else{clear.value.push(key);values[key]=''}}
onMounted(load)
</script>
<template>
 <section class="connection-settings">
  <h2>騰訊 OCR</h2><p class="config-help">密鑰只保存在此電腦，不會顯示已保存內容，也不會隨帳簿備份或發行包分享。</p>
  <el-alert v-if="error" :title="error" type="error" :closable="false"/>
    <el-form-item v-for="field in ocrFields" :key="field.key" :label="field.label"><div class="secret-line"><el-input v-model="values[field.key]" type="password" show-password autocomplete="new-password" :disabled="busy||state.locked[field.key]||clear.includes(field.key)" :placeholder="state.configured[field.key]?'已配置，留空保留原值':'填寫 '+field.label"/><el-button v-if="state.configured[field.key]&&!state.locked[field.key]" text :type="clear.includes(field.key)?'warning':'danger'" @click="toggleClear(field.key)">{{clear.includes(field.key)?'取消清除':'清除'}}</el-button></div><small v-if="state.locked[field.key]">由系統環境變數管理</small></el-form-item>
    <el-form-item label="服務區域（選填）"><el-input v-model="values.TENCENTCLOUD_REGION" placeholder="例如 ap-hongkong" :disabled="busy||state.locked.TENCENTCLOUD_REGION"/></el-form-item><p class="config-help">只用於掃描發票辨識；文字發票及銀行月結單始終在本地解析。</p>
  <el-button type="primary" :loading="busy" :disabled="!state.editable" @click="save">保存接口配置</el-button>
 </section>
</template>
<style scoped>
.connection-settings{border-top:1px solid #e3ebee;border-bottom:1px solid #e3ebee;margin-top:22px;padding:20px 0}.connection-settings h2{font-size:15px;margin:0 0 12px;color:#355565}.config-help{font-size:12px;color:#8295a1;line-height:1.8;margin:8px 0 18px}.secret-line{display:flex;gap:8px;width:100%}.config-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 16px}.config-grid :deep(.el-select),.config-grid :deep(.el-date-editor),.config-grid :deep(.el-input-number){width:100%}.connection-settings small{color:#8a9eaa}@media(max-width:600px){.config-grid{grid-template-columns:1fr}}
</style>
