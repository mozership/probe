import { Footer, LastUpdated, Layout, Navbar } from 'nextra-theme-docs'
import type { PageMapItem } from 'nextra'
import type { ReactNode } from 'react'
import { getPageMap } from 'nextra/page-map'
import { LocaleSwitch } from './locale-switch'
import { Logo } from './logo'

export type Locale = 'en' | 'ja'

const translations = {
  en: {
    editLink: 'Edit this page on GitHub',
    lastUpdated: 'Updated at',
    toc: 'On This Page',
    backToTop: 'Scroll to top',
  },
  ja: {
    editLink: 'GitHubでこのページを編集する',
    lastUpdated: '更新日時',
    toc: 'このページの内容',
    backToTop: 'トップへ戻る',
  },
} satisfies Record<Locale, Record<string, string>>

const rootRoute = {
  en: '/',
  ja: '/ja',
} satisfies Record<Locale, string>

async function pageMapOf(locale: Locale): Promise<PageMapItem[]> {
  if (locale === 'ja') {
    return getPageMap('/ja')
  }
  const pageMap = await getPageMap('/')
  return pageMap.filter(item => !('name' in item && item.name === 'ja'))
}

function collectRoutes(items: PageMapItem[], routes = new Set<string>()): Set<string> {
  for (const item of items) {
    if ('route' in item && item.route) {
      routes.add(item.route)
    }
    if ('children' in item && item.children) {
      collectRoutes(item.children, routes)
    }
  }
  return routes
}

/**
 * The English and Japanese trees are not a perfect mirror of each other, so fall
 * back to the top page of the target locale when the counterpart is missing.
 */
async function counterpartHref(locale: Locale, route: string): Promise<string> {
  const target: Locale = locale === 'ja' ? 'en' : 'ja'
  const candidate =
    locale === 'ja' ? route.replace(/^\/ja(?=\/|$)/, '') || '/' : route === '/' ? '/ja' : `/ja${route}`
  const routes = collectRoutes(await pageMapOf(target))
  return routes.has(candidate) ? candidate : rootRoute[target]
}

export async function DocsLayout({
  locale,
  route,
  children,
}: {
  locale: Locale
  route: string
  children: ReactNode
}) {
  const t = translations[locale]
  const pageMap = await pageMapOf(locale)
  const counterpart = await counterpartHref(locale, route)
  const hrefs =
    locale === 'ja'
      ? { en: counterpart, ja: route }
      : { en: route, ja: counterpart }

  return (
    <Layout
      pageMap={pageMap}
      docsRepositoryBase="https://github.com/mozership/probe/blob/main/docs"
      editLink={t.editLink}
      lastUpdated={<LastUpdated locale={locale}>{t.lastUpdated}</LastUpdated>}
      feedback={{ content: null }}
      sidebar={{ defaultMenuCollapseLevel: 2, toggleButton: false }}
      toc={{ title: t.toc, backToTop: t.backToTop }}
      navbar={
        <Navbar
          logo={<Logo />}
          logoLink={rootRoute[locale]}
          projectLink="https://github.com/mozership/probe"
        >
          <LocaleSwitch current={locale} hrefs={hrefs} />
        </Navbar>
      }
      footer={
        <Footer>
          {/* Footer lays out its children with flex, which drops the space
              between them, so the text and the link sit in one span. */}
          <span>
            Copyright © 2025-present{' '}
            <a href="https://tomohisaoda.com/projects" target="_blank" rel="noreferrer">
              linyows
            </a>
          </span>
        </Footer>
      }
    >
      {children}
    </Layout>
  )
}
