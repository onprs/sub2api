import type Icon from '@/components/icons/Icon.vue'

export interface JourneySegment {
  start: number
  readStart: number
  readEnd: number
  exitStart: number
  end: number
  overflow: number
}

export interface JourneySample {
  index: number
  next: number
  blend: number
  offsets: number[]
}

export interface JourneyBox {
  x: number
  y: number
  width: number
  height: number
}
export interface JourneyPoint { x: number; y: number }
export interface JourneyCore extends JourneyBox {
  radius: number
  fill: number
  outline: number
  opacity: number
  fontSize: number
  label: string
}
export interface JourneyShape {
  core: JourneyCore
  lines: JourneyPoint[][]
  lineOpacity: number
}
export interface JourneyTitle extends JourneyBox {
  text: string
  fontSize: number
  lineHeight: number
  weight: number
  lineWidth: number
}
export interface JourneyItem extends JourneyTitle {
  icon?: InstanceType<typeof Icon>['$props']['name']
  caption?: string
  color: string
  fontFamily: string
}
export interface JourneyFrame {
  chapter: number
  width: number
  fromItems: JourneyItem[]
  toItems: JourneyItem[]
  fromTitle: JourneyTitle
  toTitle: JourneyTitle
  surface: JourneyBox & { color: string }
  height: number
  from: JourneyShape
  to: JourneyShape
  blend: number
  ink: string
  accent: string
  paper: string
  progress: number
}

export const clamp = (value: number) => Math.min(1, Math.max(0, value))
export const mix = (from: number, to: number, amount: number) => from + (to - from) * amount
export const ease = (value: number) => {
  const t = clamp(value)
  return t * t * (3 - 2 * t)
}

// 阅读距离取自实际布局；短章保留停留段，长章不压缩正文。
export function createJourneyTimeline(heights: number[], viewport: number): JourneySegment[] {
  let start = 0
  const height = Math.max(1, viewport)
  return heights.map((contentHeight, index) => {
    const overflow = Math.max(0, contentHeight - height)
    const readStart = start + height * (index === 0 ? 0.08 : 0.16)
    const readEnd = readStart + overflow
    const exitStart = readEnd + height * (index === 0 ? 0.12 : 0.20)
    const end = exitStart + (index === heights.length - 1 ? 0 : height * 1.18)
    const result = { start, readStart, readEnd, exitStart, end, overflow }
    start = end
    return result
  })
}

export function sampleJourney(segments: JourneySegment[], distance: number): JourneySample {
  const index = Math.max(0, segments.findIndex((segment, i) => distance < segment.end || i === segments.length - 1))
  const segment = segments[index]
  if (!segment) return { index: 0, next: 0, blend: 0, offsets: [] }
  const next = Math.min(index + 1, segments.length - 1)
  const blend = next === index ? 0 : clamp((distance - segment.exitStart) / (segment.end - segment.exitStart))
  return {
    index, next, blend,
    offsets: segments.map(item => Math.min(item.overflow, Math.max(0, distance - item.readStart))),
  }
}

export function mixColor(from: string, to: string, amount: number) {
  const channels = [0, 2, 4].map(offset => Math.round(mix(
    parseInt(from.slice(offset + 1, offset + 3), 16),
    parseInt(to.slice(offset + 1, offset + 3), 16), amount,
  )))
  return `#${channels.map(channel => channel.toString(16).padStart(2, '0')).join('')}`
}

export function morphCoreBox(from: JourneyBox, to: JourneyBox, blend: number, height: number): JourneyBox {
  const amount = ease(blend)
  const box = interpolateBox(from, to, amount)
  box.y += Math.sin(amount * Math.PI) * (height * 0.58 - box.y)
  return box
}

export function interpolateBox(from: JourneyBox, to: JourneyBox, amount: number): JourneyBox {
  return {
    x: mix(from.x, to.x, amount), y: mix(from.y, to.y, amount),
    width: mix(from.width, to.width, amount), height: mix(from.height, to.height, amount),
  }
}
