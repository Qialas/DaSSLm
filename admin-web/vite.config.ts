import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import ElementPlus from 'unplugin-element-plus/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'

// 版本号统一以 package.json 为准
const { version } = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8')) as { version: string }

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const resolve = (path: string) => fileURLToPath(new URL(path, import.meta.url))
  return {
    envPrefix: ['VITE_', 'UMI_APP_'],
    define: { __APP_VERSION__: JSON.stringify(version) },
    base: '/',
    plugins: [
      vue(),
      tailwindcss(),
      AutoImport({
        imports: ['vue', 'vue-router', 'pinia', '@vueuse/core'],
        dts: 'src/types/import/auto-imports.d.ts',
        resolvers: [ElementPlusResolver()],
        include: [/\.[tj]sx?$/, /\.vue$/, /\.vue\?vue/]
      }),
      Components({ dts: 'src/types/import/components.d.ts', resolvers: [ElementPlusResolver()] }),
      ElementPlus({ useSource: true })
    ],
    resolve: {
      alias: {
        '@': resolve('./src'),
        '@views': resolve('./src/views'),
        '@imgs': resolve('./src/assets/images'),
        '@icons': resolve('./src/assets/icons'),
        '@utils': resolve('./src/utils'),
        '@stores': resolve('./src/store'),
        '@plugins': resolve('./src/plugins'),
        '@styles': resolve('./src/assets/styles')
      }
    },
    server: {
      port: 5173,
      proxy: {
        '/api': { target: env.VITE_API_PROXY_URL || 'http://127.0.0.1:8080', changeOrigin: true }
      }
    },
    build: {
      outDir: 'dist',
      target: 'es2020',
      rollupOptions: {
        output: {
          // Keep the JS assets at the root, matching the Go embedded asset server.
          entryFileNames: 'dasslm-[name]-[hash].js',
          chunkFileNames: 'dasslm-[name]-[hash].js',
          assetFileNames: 'assets/dasslm-[name]-[hash][extname]'
        }
      }
    },
    css: {
      preprocessorOptions: {
        scss: {
          additionalData: '@use "@styles/core/el-light.scss" as *; @use "@styles/core/mixin.scss" as *;'
        }
      }
    }
  }
})
