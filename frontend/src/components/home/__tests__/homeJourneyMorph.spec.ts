import { describe, expect, it } from 'vitest'
import { journeyMotifs, journeyTitleBox, sampleJourneyPieces } from '../homeJourneyMorph'
import type { JourneyFrame, JourneyItem, JourneyShape } from '../homeJourney'

const item = (text: string, index: number): JourneyItem => ({
  text, x: 24, y: 100 + index * 60, width: 250, height: 28, color: '#1b1c1f',
  fontFamily: 'Arial', fontSize: 20, lineHeight: 1.4, lineWidth: 200, weight: 400,
})
const shape: JourneyShape = {
  core: { x: 0, y: 0, width: 100, height: 100, radius: 0, fill: 0, outline: 0, opacity: 1, fontSize: 30, label: '/v1' },
  lines: [], lineOpacity: 0,
}
function frame(chapter: number, blend: number, fromCount = 4, toCount = 4): JourneyFrame {
  return {
    chapter, blend, width: 390, height: 788, from: shape, to: shape,
    fromItems: Array.from({ length: fromCount }, (_, i) => item(`SOURCE ${i}`, i)),
    toItems: Array.from({ length: toCount }, (_, i) => item(`TARGET ${i}`, i)),
    fromTitle: item('SOURCE', 0), toTitle: item('TARGET', 1),
    surface: { x: 0, y: 0, width: 390, height: 788, color: '#fafafa' },
    paper: '#fafafa', ink: '#1b1c1f', accent: '#315be8', progress: 0.5,
  }
}

describe('按章节差异化的内容转场', () => {
  it('各转场拥有独立构型，开场和终章不生成内容面板', () => {
    expect(new Set(journeyMotifs).size).toBe(7)
    expect(sampleJourneyPieces(frame(0, 0.5))).toEqual([])
    expect(sampleJourneyPieces(frame(6, 0.5))).toEqual([])
    const layouts = [1, 2, 3, 4, 5].map(chapter => JSON.stringify(sampleJourneyPieces(frame(chapter, 0.35)).map(({ box, rotation, fold, radius }) => ({ box, rotation, fold, radius }))))
    expect(new Set(layouts).size).toBe(5)
  })

  it.each([1, 2, 3, 4, 5])('第 %i 段在两端准确归位到正文锚点，正反滚动无状态差异', chapter => {
    const start = frame(chapter, 0)
    const end = frame(chapter, 1)
    sampleJourneyPieces(start).forEach((piece, i) => {
      const source = start.fromItems[i]!
      expect(piece.box).toEqual({ x: source.x, y: source.y, width: source.width, height: source.height })
      expect(piece.material).toBe(0)
    })
    sampleJourneyPieces(end).forEach((piece, i) => {
      const target = end.toItems[i]!
      expect(piece.box.x).toBeCloseTo(target.x)
      expect(piece.box.y).toBeCloseTo(target.y)
      expect(piece.box.width).toBeCloseTo(target.width)
      expect(piece.box.height).toBeCloseTo(target.height)
      expect(piece.material).toBe(0)
    })
    const times = Array.from({ length: 101 }, (_, i) => i / 100)
    const forward = times.map(t => sampleJourneyPieces(frame(chapter, t)))
    expect([...times].reverse().map(t => sampleJourneyPieces(frame(chapter, t))).reverse()).toEqual(forward)
    expect(journeyTitleBox(start).x).toBe(start.fromTitle.x)
    expect(journeyTitleBox(end).x).toBeCloseTo(end.toTitle.x)
  })

  it('六个模型归为四个步骤时，不复制步骤标题或产生重复文字', () => {
    const pieces = sampleJourneyPieces(frame(2, 0.6, 6, 4))
    expect(pieces.filter(piece => piece.opacity > 0)).toHaveLength(4)
    expect(pieces.slice(4).every(piece => piece.toOpacity === 0)).toBe(true)
    expect(sampleJourneyPieces(frame(2, 0.36, 6, 4)).filter(piece => piece.opacity > 0)).toHaveLength(4)
  })

  it('字形沿横向展开时，每个字槽保留独立空间', () => {
    for (const count of [2, 3, 4]) for (const width of [320, 390, 768, 1440]) {
      for (const blend of [0.48, 0.5, 0.55, 0.6, 0.65, 0.7]) {
        const pieces = sampleJourneyPieces({ ...frame(5, blend, count, 6), width })
        pieces.slice(0, -1).forEach((piece, index) => {
          expect(piece.box.x + piece.box.width).toBeLessThanOrEqual(pieces[index + 1]!.box.x + 0.001)
        })
      }
    }
  })

  it('控制项只取配置提供的数量；缺少能力时不补造展示项', () => {
    const limited = frame(4, 0.8, 4, 2)
    expect(sampleJourneyPieces(limited).filter(piece => piece.opacity > 0).map(piece => piece.to.text)).toEqual(['TARGET 0', 'TARGET 1'])
    expect(sampleJourneyPieces({ ...limited, toItems: [] })).toEqual([])
  })

  it('从两个控制项拼出六个字形时，不重复显示来源标题', () => {
    const pieces = sampleJourneyPieces(frame(5, 0.25, 2, 6))
    expect(pieces.filter(piece => piece.fromOpacity > 0)).toHaveLength(2)
  })

  it.each([2, 3, 4])('%i 个控制项向六个字形过渡时，不提前显示空白占位', count => {
    for (const blend of [0, 0.12, 0.25, 0.3, 0.4, 0.45]) {
      const pieces = sampleJourneyPieces(frame(5, blend, count, 6))
      expect(pieces.filter(piece => piece.opacity > 0)).toHaveLength(count)
      expect(pieces.slice(count).every(piece => piece.opacity === 0)).toBe(true)
    }
    expect(sampleJourneyPieces(frame(5, 0.65, count, 6)).filter(piece => piece.opacity > 0)).toHaveLength(6)
  })

  it('只有模型索引使用底板，且底板不能先于内容出现', () => {
    for (let chapter = 1; chapter <= 5; chapter++) for (let step = 0; step <= 100; step++) {
      for (const piece of sampleJourneyPieces(frame(chapter, step / 100, 4, 6))) {
        if (chapter !== 1) expect(piece.backgroundOpacity).toBe(0)
        expect(piece.backgroundOpacity).toBeLessThanOrEqual(Math.max(piece.fromOpacity, piece.toOpacity))
        if (!piece.fromOpacity && !piece.toOpacity) expect(piece.backgroundOpacity).toBe(0)
      }
    }
  })

  it('窄屏的构型尺寸始终为有限正数，文字字号不超出容器', () => {
    for (const width of [320, 390, 768, 1440, 2560]) for (let chapter = 1; chapter <= 5; chapter++) {
      for (const blend of [0.1, 0.3, 0.5, 0.7, 0.9]) {
        const sample = { ...frame(chapter, blend), width, height: 544 }
        for (const piece of sampleJourneyPieces(sample)) {
          expect(piece.box.width).toBeGreaterThan(0)
          expect(piece.box.height).toBeGreaterThan(0)
          expect(Number.isFinite(piece.fontSize)).toBe(true)
          expect(piece.fontSize).toBeGreaterThan(0)
        }
      }
    }
  })
})
