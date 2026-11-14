import { defineConfig } from 'vitepress'

const zhNav = [
  { text: '指南', link: '/guide/introduction' },
  { text: '功能', link: '/features/dashboard' },
  { text: '运维', link: '/operations/deployment' },
  { text: '开发', link: '/developer/architecture' },
]

const enNav = [
  { text: 'Guide', link: '/en/guide/introduction' },
  { text: 'Features', link: '/en/features/dashboard' },
  { text: 'Operations', link: '/en/operations/deployment' },
  { text: 'Developer', link: '/en/developer/architecture' },
]

const zhSidebar = [
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
      { text: 'Podman 容器', link: '/features/podman' },
      { text: '镜像管理', link: '/features/images' },
      { text: '网络与路由', link: '/features/networking' },
      { text: '快照管理', link: '/features/snapshots' },
      { text: '磁盘、备份与克隆', link: '/features/storage-backups' },
      { text: '任务与历史指标', link: '/features/tasks' },
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
]

const enSidebar = [
  {
    text: 'Get Started',
    items: [
      { text: 'Introduction', link: '/en/guide/introduction' },
      { text: 'Installation', link: '/en/guide/installation' },
      { text: 'Upgrade', link: '/en/guide/upgrade' },
      { text: 'Quick Start', link: '/en/guide/quick-start' },
      { text: 'Configuration', link: '/en/guide/configuration' },
    ],
  },
  {
    text: 'Features',
    items: [
      { text: 'Dashboard', link: '/en/features/dashboard' },
      { text: 'Containers', link: '/en/features/containers' },
      { text: 'Podman Containers', link: '/en/features/podman' },
      { text: 'Images', link: '/en/features/images' },
      { text: 'Networking & Routing', link: '/en/features/networking' },
      { text: 'Snapshots', link: '/en/features/snapshots' },
      { text: 'Disks, Backups & Clones', link: '/en/features/storage-backups' },
      { text: 'Tasks & Metric History', link: '/en/features/tasks' },
      { text: 'Sub-users', link: '/en/features/sub-users' },
      { text: 'API Integration', link: '/en/features/api' },
      { text: 'Host Report', link: '/en/features/host-report' },
    ],
  },
  {
    text: 'Operations',
    items: [
      { text: 'Deployment', link: '/en/operations/deployment' },
      { text: 'Troubleshooting', link: '/en/operations/troubleshooting' },
      { text: 'FAQ', link: '/en/operations/faq' },
    ],
  },
  {
    text: 'Developer',
    items: [
      { text: 'Architecture', link: '/en/developer/architecture' },
      { text: 'Local Build', link: '/en/developer/build' },
      { text: 'Release Process', link: '/en/developer/release' },
    ],
  },
]

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
  vite: {
    esbuild: {
      supported: {
        destructuring: true,
      },
    },
  },
  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      description: '面向 LXC/KVM 的轻量虚拟化管理面板文档',
      themeConfig: {
        nav: zhNav,
        sidebar: zhSidebar,
        outline: {
          label: '页面导航',
        },
        darkModeSwitchLabel: '外观',
        sidebarMenuLabel: '菜单',
        returnToTopLabel: '返回顶部',
      },
    },
    en: {
      label: 'English',
      lang: 'en-US',
      link: '/en/',
      description: 'Documentation for the lightweight LXC/KVM virtualization management panel.',
      themeConfig: {
        nav: enNav,
        sidebar: enSidebar,
        outline: {
          label: 'On This Page',
        },
        darkModeSwitchLabel: 'Appearance',
        sidebarMenuLabel: 'Menu',
        returnToTopLabel: 'Return to Top',
        footer: {
          message: 'CLICD documentation for deployment, usage, operations, and integration.',
          copyright: 'Copyright © CLICD contributors',
        },
      },
    },
  },
  themeConfig: {
    logo: '/favicon.svg',
    search: {
      provider: 'local',
    },
    socialLinks: [
      { icon: 'github', link: 'https://github.com/MengMengCode/CLICD' },
    ],
    footer: {
      message: 'CLICD 文档面向部署、使用、运维和二次开发场景。',
      copyright: 'Copyright © CLICD contributors',
    },
  },
})
