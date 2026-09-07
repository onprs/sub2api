import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import HomeJourneyArtwork from '../HomeJourneyArtwork.vue'
import type { JourneyFrame, JourneyShape } from '../homeJourney'

const shape = (x: number, label: string): JourneyShape => ({
  core: { x, y: 240, width: 120, height: 100, radius: 0, fill: 1, outline: 0, opacity: 1, fontSize: 40, label },
  lines: Array.from({ length: 12 }, () => [{ x, y: 200 }, { x: x + 20, y: 220 }, { x: x + 40, y: 220 }, { x: x + 60, y: 240 }]),
  lineOpacity: 0.8,
})
const frame: JourneyFrame = {
  chapter: 0, width: 1440, fromItems: [], toItems: [],
  from: shape(80, '/v1'), to: shape(400, '15'), blend: 0,
  paper: '#fafafa', ink: '#1b1c1f', accent: '#315be8', progress: 0,
  fromTitle: { x: 20, y: 40, width: 400, height: 100, text: 'Sub2API', fontSize: 70, lineHeight: 1.2, weight: 500, lineWidth: 300 },
  toTitle: { x: 60, y: 100, width: 300, height: 100, text: '模型目录', fontSize: 40, lineHeight: 1.2, weight: 500, lineWidth: 160 },
  surface: { x: 80, y: 240, width: 120, height: 100, color: '#17181b' }, height: 900,
}

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

function setup() {
  const context = {
    setTransform: vi.fn(), clearRect: vi.fn(), beginPath: vi.fn(), moveTo: vi.fn(), lineTo: vi.fn(), stroke: vi.fn(), setLineDash: vi.fn(),
  }
  vi.spyOn(HTMLCanvasElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 1440, height: 900 } as DOMRect)
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context as unknown as CanvasRenderingContext2D)
  const disconnect = vi.fn()
  vi.stubGlobal('ResizeObserver', class { observe = vi.fn(); disconnect = disconnect })
  return { context, disconnect }
}

describe('持久共享图形', () => {
  it('复用同一个 DOM 节点并在往返滚动后恢复原来的位置与路径', async () => {
    const { context, disconnect } = setup()
    const wrapper = mount(HomeJourneyArtwork, { props: { frame } })
    const core = wrapper.find('.journey-core').element
    const initialStyle = wrapper.find('.journey-core').attributes('style')
    const initialLines = context.lineTo.mock.calls.slice()
    await wrapper.setProps({ frame: { ...frame, blend: 0.4, progress: 0.3 } })
    expect(wrapper.find('.journey-core').element).toBe(core)
    expect(wrapper.find('.journey-core').attributes('style')).not.toBe(initialStyle)
    expect(wrapper.find('.journey-title').text()).toContain('Sub2API')
    context.lineTo.mockClear()
    await wrapper.setProps({ frame })
    expect(wrapper.find('.journey-core').attributes('style')).toBe(initialStyle)
    expect(context.lineTo.mock.calls).toEqual(initialLines)
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })

  it('每一帧先清理画布，避免正反滚动残影', async () => {
    const { context } = setup()
    const wrapper = mount(HomeJourneyArtwork, { props: { frame } })
    const calls = context.clearRect.mock.calls.length
    await wrapper.setProps({ frame: { ...frame, blend: 0.8 } })
    expect(context.clearRect.mock.calls.length).toBe(calls + 2)
    expect(context.clearRect.mock.calls[calls]).toEqual([0, 0, 1440, 900])
    expect(context.clearRect.mock.calls[calls + 1]![2]).toBeLessThan(1440)
    wrapper.unmount()
  })

  it('后续章节停止绘制折线，改为独立的内容形变层', async () => {
    const { context } = setup()
    const wrapper = mount(HomeJourneyArtwork, { props: { frame } })
    context.lineTo.mockClear()
    await wrapper.setProps({ frame: { ...frame, chapter: 3, blend: 0.6 } })
    expect(context.lineTo).not.toHaveBeenCalled()
    expect(wrapper.attributes('data-motif')).toBe('symbols')
    expect(wrapper.find('.journey-pieces').exists()).toBe(true)
    wrapper.unmount()
  })

  it.each([2, 3, 4, 5])('第 %i 段不再渲染拼贴卡片底板', chapter => {
    setup()
    const source = { ...frame.fromTitle, color: '#1b1c1f', fontFamily: 'Arial' }
    const target = { ...source, text: 'READY' }
    const wrapper = mount(HomeJourneyArtwork, { props: { frame: { ...frame, chapter, blend: 0.3, fromItems: [source], toItems: [target] } } })
    expect(wrapper.find('.journey-piece').exists()).toBe(true)
    expect(wrapper.find('.journey-piece-material').exists()).toBe(false)
    wrapper.unmount()
  })

  it('没有 Canvas 支持时仍然保留 DOM 节点和连续标题', () => {
    setup()
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
    const wrapper = mount(HomeJourneyArtwork, { props: { frame: { ...frame, blend: 0.6 } } })
    expect(wrapper.find('.journey-core').exists()).toBe(true)
    expect(wrapper.find('.journey-title').text()).toContain('模型目录')
    wrapper.unmount()
  })
})
