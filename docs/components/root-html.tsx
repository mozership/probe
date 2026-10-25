import { Head } from 'nextra/components'
import { notoSansJP, oswald } from './fonts'
import { Starfield } from './starfield'
import type { ReactNode } from 'react'

export function RootHtml({ lang, children }: { lang: 'en' | 'ja'; children: ReactNode }) {
  return (
    <html lang={lang} dir="ltr" suppressHydrationWarning>
      <Head
        color={{ hue: 204, saturation: 100, lightness: { light: 33, dark: 68 } }}
        backgroundColor={{ light: '#d7efff', dark: '#233643' }}
      >
        <link rel="icon" href="/logo.svg" type="image/svg+xml" />
      </Head>
      <body className={`${oswald.variable} ${notoSansJP.variable}`}>
        <Starfield />
        {children}
      </body>
    </html>
  )
}
