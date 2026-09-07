import { describe, expect, it } from 'vitest'
import { createJourneyTimeline, ease, interpolateBox, mixColor, sampleJourney } from '../homeJourney'

describe('首页连续转场时间轴', () => {
  const heights = [896, 902, 2414, 1209, 896, 896, 896]
  const timeline = createJourneyTimeline(heights, 896)

  it('为长章保留全部阅读距离并连接六段转场', () => {
    expect(timeline).toHaveLength(7)
    expect(timeline[2]!.overflow).toBe(1518)
    for (let i = 1; i < timeline.length; i++) expect(timeline[i]!.start).toBe(timeline[i - 1]!.end)
    expect(timeline[6]!.exitStart).toBe(timeline[6]!.end)
  })

  it('阅读完成后才进入转场，下一幕从顶部进入', () => {
    const segment = timeline[2]!
    const reading = sampleJourney(timeline, segment.readStart + 1000)
    expect(reading.index).toBe(2)
    expect(reading.offsets[2]).toBe(1000)
    expect(reading.offsets[3]).toBe(0)
    expect(reading.blend).toBe(0)
    const transition = sampleJourney(timeline, (segment.exitStart + segment.end) / 2)
    expect(transition.blend).toBeCloseTo(0.5)
    expect(transition.offsets[2]).toBe(1518)
  })

  it('向前或向后到达同一位置时得到完全相同的中间态', () => {
    const positions = Array.from({ length: 301 }, (_, i) => timeline[6]!.end * i / 300)
    const forward = positions.map(position => sampleJourney(timeline, position))
    const reversed = [...positions].reverse().map(position => sampleJourney(timeline, position)).reverse()
    expect(reversed).toEqual(forward)
  })

  it('精确跨越边界时只激活下一幕，终章不产生空白场景', () => {
    timeline.slice(0, -1).forEach((segment, index) => {
      const sample = sampleJourney(timeline, segment.end)
      expect(sample.index).toBe(index + 1)
      expect(sample.blend).toBe(0)
    })
    expect(sampleJourney(timeline, 1e9).index).toBe(6)
    expect(sampleJourney(timeline, -10).offsets.every(offset => offset === 0)).toBe(true)
    expect(sampleJourney([], 0).offsets).toEqual([])
  })

  it('尺寸变化重新计算阅读段，不丢失任何正文', () => {
    const narrow = createJourneyTimeline([900, 1300, 3000], 580)
    expect(narrow.map(segment => segment.overflow)).toEqual([320, 720, 2420])
    expect(narrow.every(segment => Number.isFinite(segment.end))).toBe(true)
  })

  it('锚点插值的首尾与实际 DOM 尺寸严格相同', () => {
    const from = { x: 5, y: 10, width: 80, height: 80 }
    const to = { x: 100, y: 200, width: 240, height: 50 }
    expect(interpolateBox(from, to, ease(0))).toEqual(from)
    expect(interpolateBox(from, to, ease(1))).toEqual(to)
    expect(ease(-10)).toBe(0)
    expect(ease(10)).toBe(1)
    expect(mixColor('#000000', '#ffffff', 0.5)).toBe('#808080')
  })
})
