/*
 * How an action reaches Probe. Built from `internal/actions/registry.go`, `action/serve.go`
 * (the go-plugin handshake and gRPC server) and each action's own `main.go`,
 * which calls `plugin.Serve` exactly as an out-of-tree plugin would.
 *
 * `hello` is built in too but left out here: the demo action says little
 * about the protocol.
 * The link under the diagram goes to the full list.
 */

const BUILT_IN = [
  'http',
  'shell',
  'ssh',
  'smtp',
  'file',
  'embedded',
]

const CHIP_H = 24
const CHIP_GAP = 8
const ROW_GAP = 8
const CHAR_W = 7.3
const PAD = 22

/** Flows chips left to right, wrapping inside `width`. */
function layout(names: string[], x0: number, y0: number, width: number) {
  const out: { name: string; x: number; y: number; w: number }[] = []
  let x = x0
  let y = y0
  for (const name of names) {
    const w = name.length * CHAR_W + 20
    if (x + w > x0 + width) {
      x = x0
      y += CHIP_H + ROW_GAP
    }
    out.push({ name, x, y, w })
    x += w + CHIP_GAP
  }
  return { chips: out, height: y + CHIP_H - y0 }
}

export function PluginChart({
  title,
  runner,
  runnerNote,
  builtInLabel,
  externalLabel,
  externalNames,
  transport,
}: {
  title: string
  runner: string
  runnerNote: string
  builtInLabel: string
  externalLabel: string
  externalNames: string[]
  transport: string
}) {
  const boxX = 300
  const boxW = 504
  const built = layout(BUILT_IN, boxX + PAD, 46, boxW - PAD * 2)
  const builtH = built.height + 46 + PAD - 4
  const extY = builtH + 34
  const ext = layout(externalNames, boxX + PAD, extY + 42, boxW - PAD * 2)
  const extH = ext.height + 42 + PAD - 4
  const height = extY + extH + 4

  const runnerY = (builtH / 2 + (extY + extH / 2)) / 2 - 45

  return (
    <svg
      className="lp-flow"
      viewBox={`0 0 808 ${height}`}
      role="img"
      aria-label={title}
      preserveAspectRatio="xMidYMid meet"
    >
      <defs>
        <marker
          id="lp-plug-arrow"
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

      <path
        className="lp-flow__edge"
        d={`M194 ${runnerY + 45} C247 ${runnerY + 45}, 247 ${builtH / 2}, ${boxX} ${builtH / 2}`}
        markerEnd="url(#lp-plug-arrow)"
      />
      <path
        className="lp-flow__edge"
        d={`M194 ${runnerY + 45} C247 ${runnerY + 45}, 247 ${extY + extH / 2}, ${boxX} ${extY + extH / 2}`}
        markerEnd="url(#lp-plug-arrow)"
      />
      <text className="lp-flow__action lp-flow__transport" x="247" y={runnerY + 32} textAnchor="middle">
        {transport}
      </text>

      <rect className="lp-flow__job" x="4" y={runnerY} width="190" height="90" rx="4" />
      <text className="lp-flow__jobName" x="20" y={runnerY + 36}>
        {runner}
      </text>
      <text className="lp-flow__stepName" x="20" y={runnerY + 60}>
        {runnerNote}
      </text>

      <rect className="lp-flow__job" x={boxX} y="4" width={boxW} height={builtH} rx="4" />
      <text className="lp-flow__jobName" x={boxX + PAD} y="30">
        {builtInLabel}
      </text>
      {built.chips.map(chip => (
        <g key={chip.name}>
          <rect className="lp-flow__step" x={chip.x} y={chip.y} width={chip.w} height={CHIP_H} rx="2" />
          <text className="lp-flow__chip" x={chip.x + chip.w / 2} y={chip.y + 16} textAnchor="middle">
            {chip.name}
          </text>
        </g>
      ))}

      <rect className="lp-flow__job" x={boxX} y={extY} width={boxW} height={extH} rx="4" />
      <text className="lp-flow__jobName" x={boxX + PAD} y={extY + 26}>
        {externalLabel}
      </text>
      {ext.chips.map(chip => (
        <g key={chip.name}>
          <rect className="lp-flow__step" x={chip.x} y={chip.y} width={chip.w} height={CHIP_H} rx="2" />
          <text className="lp-flow__chip" x={chip.x + chip.w / 2} y={chip.y + 16} textAnchor="middle">
            {chip.name}
          </text>
        </g>
      ))}
    </svg>
  )
}
