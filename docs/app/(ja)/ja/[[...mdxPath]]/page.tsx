import { generateStaticParamsFor, importPage } from 'nextra/pages'
import { DocsLayout } from '../../../../components/docs-page'
import { useMDXComponents as getMDXComponents } from '../../../../mdx-components'

type Params = { mdxPath?: string[] }

export async function generateStaticParams() {
  const params = await generateStaticParamsFor('mdxPath')()
  return params
    .filter((param: Params) => param.mdxPath?.[0] === 'ja')
    .map((param: Params) => ({ mdxPath: param.mdxPath!.slice(1) }))
}

export async function generateMetadata(props: { params: Promise<Params> }) {
  const params = await props.params
  const { metadata } = await importPage(['ja', ...(params.mdxPath ?? [])])
  // The home page gives its whole title; the layout's template would put
  // "| Probe" after one that already starts with the name.
  if (!params.mdxPath?.length) return { ...metadata, title: { absolute: metadata.title } }
  return metadata
}

const Wrapper = getMDXComponents().wrapper

export default async function Page(props: { params: Promise<Params> }) {
  const params = await props.params
  const mdxPath = ['ja', ...(params.mdxPath ?? [])]
  const { default: MDXContent, toc, metadata } = await importPage(mdxPath)
  const route = `/${mdxPath.join('/')}`

  return (
    <DocsLayout locale="ja" route={route}>
      <Wrapper toc={toc} metadata={metadata}>
        <MDXContent {...props} params={params} />
      </Wrapper>
    </DocsLayout>
  )
}
