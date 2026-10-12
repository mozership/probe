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
    title: 'Guide',
  },
  reference: {
    type: 'page',
    title: 'Reference',
  },
  external: {
    type: 'menu',
    title: 'External',
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
  ja: {
    type: 'page',
    display: 'hidden',
  },
} satisfies MetaRecord
