/*
 * The job graph of `heroWorkflow`, drawn from the `flowchart LR` that
 * `probe dag --mermaid` emits for it: one box per job, the step inside it,
 * and an edge per `needs`. The action name on each box is read from the
 * workflow, and the icon beside the job name says what the job drives: a
 * browser, an API, a database, a mailbox, a phone, or a
 * person the agent action plays. Those two are the
 * additions to what Mermaid would draw.
 *
 * Inline SVG rather than mermaid.js: the graph is fixed, and a megabyte of
 * client-side layout engine to draw seven boxes is not a trade worth making.
 */

type Target = 'browser' | 'api' | 'db' | 'mail' | 'phone' | 'persona'

type Node = {
  id: string
  job: string
  step: string
  action: string
  target: Target
  x: number
  y: number
}

const W = 196
const H = 76

// The left edge of each column, and the top of a box centred on the graph.
const COL = [4, 296, 588, 880]
const MID = 108

const nodes: Node[] = [
  { id: 'signup', job: 'Sign up', step: 'Fill in the form', action: 'browser', target: 'browser', x: COL[0], y: MID },
  { id: 'api', job: 'API', step: 'Sign-in is refused', action: 'http', target: 'api', x: COL[1], y: 4 },
  { id: 'db', job: 'Database', step: 'Row is pending', action: 'db', target: 'db', x: COL[1], y: MID },
  { id: 'mailbox', job: 'Mailbox', step: 'Confirmation arrived', action: 'imap', target: 'mail', x: COL[1], y: 212 },
  { id: 'activate', job: 'Activate', step: 'Follow the link', action: 'browser', target: 'browser', x: COL[2], y: MID },
  { id: 'ios', job: 'iOS app', step: 'Sign in on iPhone', action: 'ios', target: 'phone', x: COL[3], y: 56 },
  { id: 'visit', job: 'First visit', step: 'Ada names herself', action: 'agent', target: 'persona', x: COL[3], y: 160 },
]

const needs: [string, string][] = [
  ['signup', 'api'],
  ['signup', 'db'],
  ['signup', 'mailbox'],
  ['api', 'activate'],
  ['db', 'activate'],
  ['mailbox', 'activate'],
  ['activate', 'ios'],
  ['activate', 'visit'],
]

const byId = Object.fromEntries(nodes.map(n => [n.id, n]))

/*
 * Line drawings on a 24-unit grid, stroked in one color so that they read as
 * kinds of target and not as brands.
 */
const ICONS: Record<Target, string[]> = {
  browser: ['M4 4h16a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1z', 'M3 9h18', 'M6 6.5h.01', 'M8.5 6.5h.01'],
  api: [
    'M8 4c-1.7 0-2 .9-2 2.5v3c0 1.3-.7 2.5-2 2.5 1.3 0 2 1.2 2 2.5v3c0 1.6.3 2.5 2 2.5',
    'M16 4c1.7 0 2 .9 2 2.5v3c0 1.3.7 2.5 2 2.5-1.3 0-2 1.2-2 2.5v3c0 1.6-.3 2.5-2 2.5',
  ],
  db: [
    'M5 6c0-1.7 3.1-3 7-3s7 1.3 7 3-3.1 3-7 3-7-1.3-7-3z',
    'M5 6v12c0 1.7 3.1 3 7 3s7-1.3 7-3V6',
    'M5 12c0 1.7 3.1 3 7 3s7-1.3 7-3',
  ],
  mail: ['M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z', 'M3.5 6.5 12 13l8.5-6.5'],
  persona: ['M12 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8z', 'M4 21c0-4 3.6-7 8-7s8 3 8 7'],
  phone: ['M8.5 2h7A2.5 2.5 0 0 1 18 4.5v15a2.5 2.5 0 0 1-2.5 2.5h-7A2.5 2.5 0 0 1 6 19.5v-15A2.5 2.5 0 0 1 8.5 2z', 'M10.5 18.5h3'],
}

function Icon({ target, x, y }: { target: Target; x: number; y: number }) {
  return (
    <g className="lp-flow__icon" transform={`translate(${x} ${y}) scale(0.75)`}>
      {ICONS[target].map(d => (
        <path key={d} d={d} />
      ))}
    </g>
  )
}

export function Flowchart({ title }: { title: string }) {
  return (
    <svg
      className="lp-flow lp-flow--hero"
      viewBox={`0 0 ${COL[3] + W + 4} 292`}
      role="img"
      aria-label={title}
      preserveAspectRatio="xMidYMid meet"
    >
      <defs>
        <marker
          id="lp-flow-arrow"
          viewBox="0 0 10 10"
          refX="9"
          refY="5"
          markerWidth="4"
          markerHeight="4"
          orient="auto-start-reverse"
        >
          <path d="M0 0 L10 5 L0 10 z" className="lp-flow__head" />
        </marker>
      </defs>

      {needs.map(([from, to]) => {
        const a = byId[from]
        const b = byId[to]
        const x1 = a.x + W
        const y1 = a.y + H / 2
        const x2 = b.x
        const y2 = b.y + H / 2
        const mid = (x1 + x2) / 2
        return (
          <path
            key={`${from}-${to}`}
            className="lp-flow__edge"
            d={`M${x1} ${y1} C${mid} ${y1}, ${mid} ${y2}, ${x2} ${y2}`}
            markerEnd="url(#lp-flow-arrow)"
          />
        )
      })}

      {nodes.map(node => (
        <g key={node.id}>
          <rect className="lp-flow__job" x={node.x} y={node.y} width={W} height={H} rx="4" />
          <Icon target={node.target} x={node.x + 11} y={node.y + 7} />
          <text className="lp-flow__jobName" x={node.x + 36} y={node.y + 21}>
            {node.job}
          </text>
          <text className="lp-flow__action" x={node.x + W - 12} y={node.y + 21} textAnchor="end">
            {node.action}
          </text>
          <rect
            className="lp-flow__step"
            x={node.x + 10}
            y={node.y + 34}
            width={W - 20}
            height={30}
            rx="2"
          />
          <text className="lp-flow__stepName" x={node.x + 22} y={node.y + 54}>
            {node.step}
          </text>
        </g>
      ))}
    </svg>
  )
}
