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
    'Probe checks that software works end to end, from one YAML workflow across HTTP, gRPC, databases, mail, SSH and shell. Made for the code AI agents write: they write the workflow, and Probe checks it and runs it safely.',
}

export default function RootLayout({ children }: { children: ReactNode }) {
  return <RootHtml lang="en">{children}</RootHtml>
}
