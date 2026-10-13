import type { MetaRecord } from 'nextra'

export default {
  '-- builtin': { type: 'separator', title: 'Built-in' },
  http: 'HTTP',
  db: 'DB',
  shell: 'SHELL',
  ssh: 'SSH',
  smtp: 'SMTP',
  imap: 'IMAP',
  grpc: 'GRPC',
  dns: 'DNS',
  embedded: 'EMBEDDED',
  hello: 'HELLO',
  '-- external': { type: 'separator', title: 'External' },
  browser: 'BROWSER',
  container: 'CONTAINER',
  graphql: 'GRAPHQL',
  jmap: 'JMAP',
  'mail-latency': 'MAIL-LATENCY',
  redis: 'REDIS',
  s3: 'S3',
  websocket: 'WEBSOCKET',
} satisfies MetaRecord
