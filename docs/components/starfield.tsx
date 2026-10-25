'use client'

import { useEffect, useMemo, useState } from 'react'
import type { CSSProperties } from 'react'

type Shape = 'dot' | 'plus' | 'sparkle'

type Star = {
  shape: Shape
  x: number
  y: number
  duration: number
  delay: number
  /** How brightly it flares, as a share of the theme's peak. */
  gain: number
}

/** A small seeded generator, so the same grid always draws the same sky. */
function mulberry32(seed: number) {
  return () => {
    seed = (seed + 0x6d2b79f5) | 0
    let t = seed
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** About one star in each square of this many pixels, at any window size. */
const SPACING = 116

type Grid = { cols: number; rows: number }

function gridOf(width: number, height: number): Grid {
  return {
    cols: Math.max(1, Math.round(width / SPACING)),
    rows: Math.max(1, Math.round(height / SPACING)),
  }
}

/**
 * One star in each cell of a grid sized to the window, at a jittered spot in
 * it, so the stars spread evenly without clumping and without repeating.
 */
function sky({ cols, rows }: Grid): Star[] {
  const rand = mulberry32(20251025)
  const stars: Star[] = []
  for (let i = 0; i < cols * rows; i++) {
    const col = i % cols
    const row = Math.floor(i / cols)
    const r = rand()
    stars.push({
      shape: r < 0.6 ? 'dot' : r < 0.88 ? 'plus' : 'sparkle',
      x: ((col + 0.1 + rand() * 0.8) / cols) * 100,
      y: ((row + 0.1 + rand() * 0.8) / rows) * 100,
      duration: 3 + rand() * 6,
      delay: -rand() * 9,
      gain: 0.3 + rand() * 0.7,
    })
  }
  return stars
}

/**
 * Pixel stars behind every page. Each one twinkles on its own cycle: it rests
 * faint, flares briefly, and fades back. The window's size is known only in
 * the browser, so the sky is drawn after the page loads, and drawn again when
 * a resize changes the grid.
 */
export function Starfield() {
  const [grid, setGrid] = useState<Grid | null>(null)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const measure = () => {
      const next = gridOf(window.innerWidth, window.innerHeight)
      setGrid(prev => (prev && prev.cols === next.cols && prev.rows === next.rows ? prev : next))
    }
    const onResize = () => {
      clearTimeout(timer)
      timer = setTimeout(measure, 200)
    }
    measure()
    window.addEventListener('resize', onResize)
    return () => {
      clearTimeout(timer)
      window.removeEventListener('resize', onResize)
    }
  }, [])

  const stars = useMemo(() => (grid ? sky(grid) : []), [grid])

  return (
    <div className="probe-stars" aria-hidden="true">
      {stars.map((s, i) => (
        <span
          key={i}
          className={`probe-star probe-star--${s.shape}`}
          style={
            {
              left: `${s.x.toFixed(2)}%`,
              top: `${s.y.toFixed(2)}%`,
              animationDuration: `${s.duration.toFixed(2)}s`,
              animationDelay: `${s.delay.toFixed(2)}s`,
              '--star-gain': s.gain.toFixed(2),
            } as CSSProperties
          }
        />
      ))}
    </div>
  )
}
