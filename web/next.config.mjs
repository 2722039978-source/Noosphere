/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Three.js 等重依赖只在客户端组件内动态加载，无需额外配置
};

export default nextConfig;
