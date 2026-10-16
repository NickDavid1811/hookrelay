import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({ plugins: [react()], server: { proxy: { '/play/api': { target:'http://127.0.0.1:8090',changeOrigin:true,configure(proxy){proxy.on('proxyReq',(req)=>{req.setHeader('Origin','http://127.0.0.1:8090')})} } } } });
