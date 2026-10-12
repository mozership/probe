import type { MetaRecord } from 'nextra'

export default {
  index: {
    type: 'page',
    display: 'hidden',
    theme: {
      layout: 'full',
      sidebar: false,
      toc: false,
      breadcrumb: false,
      pagination: false,
      timestamp: false,
    },
  },
  guide: {
    type: 'page',
    title: 'ガイド',
  },
  reference: {
    type: 'page',
    title: 'リファレンス',
  },
  external: {
    type: 'menu',
    title: '外部ドキュメント',
    items: {
      release: {
        title: 'Github Release',
        href: 'https://github.com/mozership/probe/releases',
      },
      godoc: {
        title: 'Go Docs',
        href: 'https://pkg.go.dev/github.com/mozership/probe',
      },
      deepwiki: {
        title: 'Deep Wiki',
        href: 'https://deepwiki.com/mozership/probe',
      },
    },
  },
} satisfies MetaRecord
