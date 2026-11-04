import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
export default defineConfig({
 plugins: [vue(), Components({ resolvers: [ElementPlusResolver()], dts: false })],
 server: { proxy: { '/api': 'http://127.0.0.1:8080' } },
 build: { outDir: 'dist', rollupOptions: { output: { manualChunks(id) {
  const path = id.replaceAll('\\', '/')
  if (path.includes('/element-plus/es/components/table/') || path.includes('/element-plus/es/components/date-picker/')) return 'element-widgets'
  if (path.includes('/node_modules/@vue/') || path.includes('/node_modules/vue/')) return 'vue'
  if (path.includes('/node_modules/@element-plus/icons-vue/')) return 'icons'
 } } } },
})


