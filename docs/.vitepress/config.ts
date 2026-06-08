import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'CLICD',
  description: '面向 LXC/KVM 的轻量虚拟化管理面板文档',
  lang: 'zh-CN',
  base: process.env.VITEPRESS_BASE || '/',
  cleanUrls: true,
  ignoreDeadLinks: true,
  head: [
    ['link', { rel: 'icon', href: '/favicon.svg' }],
  ],
  themeConfig: {
    logo: '/favicon.svg',
    search: {
      provider: 'local',
    },
    nav: [
      { text: '指南', link: '/guide/introduction' },
      { text: '功能', link: '/features/dashboard' },
      { text: '运维', link: '/operations/deployment' },
      { text: '开发', link: '/developer/architecture' },
    ],
    sidebar: [
      {
        text: '开始',
        items: [
          { text: '项目介绍', link: '/guide/introduction' },
          { text: '安装', link: '/guide/installation' },
          { text: '升级', link: '/guide/upgrade' },
          { text: '快速上手', link: '/guide/quick-start' },
          { text: '配置说明', link: '/guide/configuration' },
        ],
      },
      {
        text: '功能',
        items: [
          { text: '控制面板', link: '/features/dashboard' },
          { text: '容器管理', link: '/features/containers' },
          { text: '镜像管理', link: '/features/images' },
          { text: '网络与路由', link: '/features/networking' },
          { text: '快照管理', link: '/features/snapshots' },
          { text: '安全告警', link: '/features/security' },
          { text: '子用户', link: '/features/sub-users' },
          { text: 'API 集成', link: '/features/api' },
          { text: '主机报告', link: '/features/host-report' },
        ],
      },
      {
        text: '运维',
        items: [
          { text: '部署建议', link: '/operations/deployment' },
          { text: '故障排查', link: '/operations/troubleshooting' },
          { text: '常见问题', link: '/operations/faq' },
        ],
      },
      {
        text: '开发',
        items: [
          { text: '系统架构', link: '/developer/architecture' },
          { text: '本地构建', link: '/developer/build' },
          { text: '发布流程', link: '/developer/release' },
        ],
      },
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/MengMengCode/CLICD' },
    ],
    footer: {
      message: 'CLICD 文档面向部署、使用、运维和二次开发场景。',
      copyright: 'Copyright © CLICD contributors',
    },
  },
})
