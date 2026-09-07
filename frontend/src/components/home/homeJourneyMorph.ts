import { clamp, ease, interpolateBox, mix, type JourneyBox, type JourneyFrame, type JourneyItem } from './homeJourney'

export const journeyMotifs = ['routing', 'index', 'sequence', 'symbols', 'orbit', 'typeset', 'ready'] as const

export interface JourneyPiece {
  box: JourneyBox
  from: JourneyItem
  to: JourneyItem
  opacity: number
  material: number
  backgroundOpacity: number
  captionSize: number
  radius: number
  rotation: number
  fold: number
  padding: number
  fontSize: number
  iconSize: number
  fromOpacity: number
  toOpacity: number
}

// 所有中间态都由滚动位置直接采样，不保存方向或已播放状态。
function through(from: JourneyBox, first: JourneyBox, second: JourneyBox, to: JourneyBox, progress: number) {
  if (progress < 0.3) return interpolateBox(from, first, ease(progress / 0.3))
  if (progress < 0.7) return interpolateBox(first, second, ease((progress - 0.3) / 0.4))
  return interpolateBox(second, to, ease((progress - 0.7) / 0.3))
}

export function journeyTitleBox(frame: JourneyFrame): JourneyBox {
  if (frame.chapter === 0) return interpolateBox(frame.fromTitle, frame.toTitle, ease(frame.blend))
  const mobile = frame.width <= 760
  const middle = {
    x: mobile ? 24 : Math.max(40, frame.width * 0.045), y: mobile ? 32 : frame.height * 0.13,
    width: mobile ? frame.width - 48 : frame.width * 0.34, height: mobile ? 104 : 180,
  }
  return through(frame.fromTitle, middle, middle, frame.toTitle, frame.blend)
}

function area(frame: JourneyFrame): JourneyBox {
  const mobile = frame.width <= 760
  const y = mobile ? Math.max(176, frame.height * 0.30) : frame.height * 0.16
  return {
    x: mobile ? 24 : frame.width * 0.46, y,
    width: mobile ? frame.width - 48 : frame.width * 0.48,
    height: Math.max(180, frame.height - y - (mobile ? 76 : 120)),
  }
}

function grid(area: JourneyBox, index: number, count: number, columns: number, gap: number): JourneyBox {
  const rows = Math.ceil(count / columns)
  const width = (area.width - gap * (columns - 1)) / columns
  const height = (area.height - gap * (rows - 1)) / rows
  return { x: area.x + index % columns * (width + gap), y: area.y + Math.floor(index / columns) * (height + gap), width, height }
}

function waypoint(frame: JourneyFrame, index: number, count: number, late: boolean): JourneyBox {
  const space = area(frame)
  const mobile = frame.width <= 760
  if (frame.chapter === 1) {
    const sheet = grid(space, index, count, mobile ? 1 : 2, mobile ? 8 : 18)
    const inset = (count - index - 1) * (mobile ? 2 : 5)
    return { ...sheet, x: sheet.x + (late ? 0 : inset), width: sheet.width - (late ? 0 : inset), height: Math.min(sheet.height, mobile ? 64 : 132) }
  }
  if (frame.chapter === 2) {
    if (!late) return grid(space, index, count, 1, 8)
    const targetCount = Math.max(1, frame.toItems.length)
    const step = grid(space, index % targetCount, targetCount, 1, mobile ? 12 : 24)
    return { ...step, x: step.x + (index % targetCount) * (mobile ? 6 : 18), width: step.width - (targetCount - 1) * (mobile ? 6 : 18) }
  }
  if (frame.chapter === 3) {
    const symbol = grid(space, index, count, 2, mobile ? 24 : 44)
    return late ? symbol : { ...symbol, y: symbol.y + symbol.height * 0.2, height: symbol.height * 0.6 }
  }
  if (frame.chapter === 4) {
    const cells = grid(space, index, count, 2, mobile ? 16 : 28)
    if (late) return cells
    const angle = index / count * Math.PI * 2 - Math.PI / 2
    const size = Math.min(space.width, space.height) * 0.28
    return {
      x: space.x + space.width / 2 + Math.cos(angle) * space.width * 0.32 - size / 2,
      y: space.y + space.height / 2 + Math.sin(angle) * space.height * 0.32 - size / 2,
      width: size, height: size,
    }
  }
  // 控制项最后合为真实 READY 字形，不生成新的能力或状态文案。
  if (!late) return grid(space, index % frame.fromItems.length, frame.fromItems.length, 1, 16)
  const widths = frame.toItems.map(item => Math.max(1, item.lineWidth))
  const totalWidth = widths.reduce((sum, width) => sum + width, 0)
  return {
    x: space.x + widths.slice(0, index).reduce((sum, width) => sum + width, 0) / totalWidth * space.width,
    y: space.y + space.height * 0.36, width: widths[index]! / totalWidth * space.width, height: space.height * 0.32,
  }
}

export function sampleJourneyPieces(frame: JourneyFrame): JourneyPiece[] {
  if (frame.chapter === 0 || frame.chapter >= 6 || !frame.fromItems.length || !frame.toItems.length) return []
  const count = Math.max(frame.fromItems.length, frame.toItems.length)
  const t = clamp(frame.blend)
  const material = ease(t / 0.24) * (1 - ease((t - 0.76) / 0.24))
  return Array.from({ length: count }, (_, index) => {
    const from = frame.fromItems[index % frame.fromItems.length]!
    const to = frame.toItems[index % frame.toItems.length]!
    const first = waypoint(frame, index, count, false)
    const second = waypoint(frame, index, count, true)
    const box = through(from, first, second, to, t)
    // 字形横向展开时同步收窄字槽，避免宽控制项挤占相邻字母的位置。
    if (frame.chapter === 5 && t > 0.3 && t < 0.7) {
      const width = mix(box.width, second.width * ease((t - 0.3) / 0.4), ease((t - 0.36) / 0.10))
      box.x += (box.width - width) / 2
      box.width = width
    }
    if (frame.chapter === 2 || frame.chapter === 4 || frame.chapter === 5) {
      const travel = t < 0.3 ? t / 0.3 : (t - 0.3) / 0.4
      const contraction = 1 - Math.sin(Math.PI * ease(travel)) * 0.6
      const width = box.width * contraction
      const height = box.height * contraction
      box.x += (box.width - width) / 2
      box.y += (box.height - height) / 2
      box.width = width
      box.height = height
    }
    const active = t < 0.5 ? from : to
    const iconLimit = frame.chapter === 3 ? 112 : 72
    const iconSize = active.icon ? Math.min(box.width * 0.44, box.height * 0.44, iconLimit) * material : 0
    const padding = frame.chapter === 1 ? Math.min(box.width * 0.09, box.height * 0.10, 22) * material : 0
    const intendedSize = frame.chapter === 5 && t >= 0.5 ? Math.min(box.height * 0.82, 176) : frame.chapter === 1 || frame.chapter === 2 && t < 0.5 ? 14 : frame.chapter === 2 ? 44 : 28
    const captionSize = active.caption ? Math.min(frame.chapter === 2 && t >= 0.5 ? 44 : 16, box.height * 0.24, box.width / 10) * material : 0
    const lineHeight = active.lineHeight || 1.3
    const fontSize = Math.max(1, Math.min(
      mix(active.fontSize, intendedSize, material),
      (box.width - padding * 2) / Math.max(1, active.lineWidth) * active.fontSize,
      (box.height - padding * 2 - iconSize - (iconSize ? 6 * material : 0) - (active.caption ? captionSize + 6 * material : 0)) / Math.max(1, active.text.split('\n').length) / lineHeight,
    ))
    const fromOpacity = index >= frame.fromItems.length ? 0 : 1 - ease((t - 0.36) / 0.14)
    const toOpacity = index >= frame.toItems.length ? 0 : ease((t - 0.46) / 0.14)
    const added = index >= frame.fromItems.length ? toOpacity : 1
    const removed = index >= frame.toItems.length ? 1 - ease((t - 0.30) / 0.06) : 1
    return {
      box, from, to, material, padding, fontSize, iconSize, captionSize,
      backgroundOpacity: frame.chapter === 1 ? material * Math.max(fromOpacity, toOpacity) : 0,
      opacity: added * removed,
      radius: frame.chapter === 4 ? mix(Math.min(box.width, box.height) / 2, 4, ease((t - 0.3) / 0.4)) * material : 4 * material,
      rotation: frame.chapter === 1 ? (index % 2 ? 5 : -5) * material * (1 - ease((t - 0.45) / 0.4)) : 0,
      fold: frame.chapter === 3 ? (index % 2 ? -1 : 1) * 50 * material * (1 - ease((t - 0.3) / 0.4)) : 0,
      fromOpacity, toOpacity,
    }
  })
}
