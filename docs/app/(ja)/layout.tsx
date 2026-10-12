import type { Metadata } from 'next'
import type { ReactNode } from 'react'
import { RootHtml } from '../../components/root-html'
import 'nextra-theme-docs/style.css'
import '../globals.css'
import '../home.css'

export const metadata: Metadata = {
  title: {
    default: 'Probe',
    template: '%s | Probe',
  },
  description:
    'Probeは、ソフトウェアが動くことをEnd-to-endで確かめるツールです。HTTP、gRPC、データベース、メール、SSH、シェルにまたがる手順を1つのYAMLワークフローに書けます。AIエージェントが書くコードのために作られており、エージェントが書いたワークフローを検査し、安全に実行します。',
}

export default function RootLayout({ children }: { children: ReactNode }) {
  return <RootHtml lang="ja">{children}</RootHtml>
}
