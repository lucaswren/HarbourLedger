import {ref,watch} from 'vue'
import {t as traditional, simplified} from './i18n'
import english from './english.json'
export type Language='zh-Hant'|'zh-Hans'|'en'
const stored=localStorage.getItem('harbour-language')
export const language=ref<Language>(stored==='en'||stored==='zh-Hans'?stored:'zh-Hant')
watch(language,v=>{localStorage.setItem('harbour-language',v);document.documentElement.lang=v},{immediate:true})
const dictionary:Record<string,string>={}
for(const [k,v] of Object.entries(english))dictionary[traditional(k)]=v
export function t(value:unknown):string {
 const text=traditional(value)
 if(language.value==='zh-Hant')return text
 if(language.value==='zh-Hans')return simplified(text)
 if(dictionary[text])return dictionary[text]!
 if(/^正在/.test(text))return 'Working…'
 if(/^\d+ 條記錄$/.test(text))return text.replace('條記錄','records')
 return text.replace(/(\d+) 份/g,'$1 files').replace(/(\d+) 筆/g,'$1 transactions')
}
