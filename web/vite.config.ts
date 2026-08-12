import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		port: 5173,
		proxy: {
			'/api': {
				target: 'http://localhost:8080',
				changeOrigin: true,
				secure: false
			},
			'/healthz': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			'/version': {
				target: 'http://localhost:8080',
				changeOrigin: true
			}
		}
	},
	build: {
		target: 'es2022',
		sourcemap: false
	}
});