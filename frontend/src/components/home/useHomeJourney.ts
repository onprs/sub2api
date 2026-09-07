import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch, type CSSProperties } from 'vue'
import { createJourneyTimeline, ease, interpolateBox, morphCoreBox, sampleJourney, type JourneyBox, type JourneyFrame, type JourneyItem, type JourneySegment, type JourneyShape, type JourneyTitle } from './homeJourney'

interface SceneLayout {
  shape: JourneyShape
  pinned: boolean
  corePinned: boolean
  entryTitle: JourneyTitle
  exitTitle: JourneyTitle
  items: JourneyItem[]
}
const sceneIds = ['cover', 'protocols', 'models', 'architecture', 'semantics', 'control', 'ready']

export function useHomeJourney(isDark: () => boolean, contentKey: () => unknown) {
  const trackRef = ref<HTMLElement | null>(null)
  const stageRef = ref<HTMLElement | null>(null)
  const enabled = ref(false)
  const distance = ref(0)
  const viewport = ref(1)
  const segments = shallowRef<JourneySegment[]>([])
  const layouts = shallowRef<SceneLayout[]>([])
  let media: MediaQueryList | undefined
  let observer: ResizeObserver | undefined
  let raf: number | undefined
  let measuring = false
  let disposed = false
  const sample = computed(() => sampleJourney(segments.value, distance.value))
  const total = computed(() => segments.value.at(-1)?.end ?? 0)
  const trackStyle = computed<CSSProperties>(() => enabled.value ? { height: `${total.value + viewport.value}px` } : {})
  const phase = computed(() => sample.value.blend < 0.5 ? sample.value.index : sample.value.next)

  function elements() { return Array.from(stageRef.value?.querySelectorAll<HTMLElement>(':scope > section') ?? []) }
  function update() {
    if (!enabled.value || !trackRef.value || !stageRef.value) return
    const header = window.innerWidth <= 760 ? 56 : 64
    distance.value = Math.max(0, Math.min(total.value, header - trackRef.value.getBoundingClientRect().top))
  }
  function schedule() {
    if (raf !== undefined) return
    raf = window.requestAnimationFrame(() => { raf = undefined; update() })
  }

  function measure() {
    if (!enabled.value || !stageRef.value || measuring || disposed) return
    measuring = true
    const stage = stageRef.value
    stage.classList.add('journey-measuring')
    const { width, height } = stage.getBoundingClientRect()
    const scenes = elements()
    viewport.value = height
    const visibleHeight = height - 40
    const heights = scenes.map(scene => scene.offsetHeight)
    const mobile = window.innerWidth <= 760
    const gutter = parseFloat(getComputedStyle(scenes[1] ?? stage).paddingLeft) || 24
    const nodeSelectors = ['', '.route-primary', '.catalog-primary', '.pipeline-diagram strong', '.assurance-topline svg', '.feature-head svg', '.poster-cta-btn']
    const lineSelectors = ['', '.protocol-row', '.provider-row', '.pipeline-step-item', '.assurance-cell', '.control-feature-entry', '.poster-cta-btn']
    layouts.value = scenes.map((scene, index) => {
      const sceneRect = scene.getBoundingClientRect()
      const box = (element: Element | null): JourneyBox => {
        if (!element) return { x: gutter, y: 160, width: 40, height: 40 }
        const rect = element.getBoundingClientRect()
        return { x: rect.left - sceneRect.left, y: rect.top - sceneRect.top, width: rect.width, height: rect.height }
      }
      let coreBox = box(scene.querySelector(nodeSelectors[index] || 'h1'))
      let lines: JourneyShape['lines'] = []
      const label = ['/v1', '/v1', scene.querySelector('.catalog-primary')?.textContent ?? '15', 'IR', '', '', ''][index] ?? ''
      let fontSize = parseFloat(getComputedStyle(scene.querySelector(nodeSelectors[index] || 'h1') ?? scene).fontSize)
      if (index === 0) {
        const compact = mobile && heights[0]! < 680
        const left = mobile ? -width * 0.06 : width * 0.51
        const top = mobile ? heights[0]! - (compact ? 246 : 286) : heights[0]! * 0.44
        const unit = mobile ? Math.min(width * (compact ? 0.74 : 0.94), 420) : Math.min(width * 0.51, heights[0]! * 0.66)
        const x = left + unit * 0.52
        const y = top + unit * 0.26
        coreBox = { x: x - unit * 0.095, y: y - unit * 0.105, width: unit * 0.21, height: unit * 0.21 }
        fontSize = unit * 0.066
        lines = Array.from({ length: 12 }, (_, i) => {
          const row = i < 6 ? Math.floor(i * 4 / 6) : i % 6
          const endY = top + row * unit * (i < 6 ? 0.15 : 0.09)
          return i < 6
            ? [{ x: left - unit * 0.06, y: endY }, { x: left + unit * 0.18, y: endY }, { x: x - unit * 0.12, y: y + (row - 1.5) * 6 }, { x, y: y + (row - 1.5) * 6 }]
            : [{ x, y: y + (row - 2.5) * 6 }, { x: x + unit * 0.2, y: y + (row - 2.5) * 6 }, { x: x + unit * 0.39, y: endY }, { x: width, y: endY }]
        })
      } else {
        const rows = Array.from(scene.querySelectorAll(lineSelectors[index]!)).map(box)
        lines = Array.from({ length: 12 }, (_, i) => {
          const rect = rows[i % rows.length] ?? coreBox
          if (index === 6) {
            const x = rect.x + rect.width / 2
            const y = rect.y + rect.height / 2
            return [{ x, y }, { x, y }, { x, y }, { x, y }]
          }
          return [0, 1 / 3, 2 / 3, 1].map(t => ({ x: rect.x + rect.width * t, y: rect.y }))
        })
      }
      const intro = scene.querySelector<HTMLElement>('.model-intro, .pipeline-intro')
      const introBox = intro ? box(intro) : null
      const pinned = !!introBox && !mobile && introBox.y + introBox.height < visibleHeight - 24
      scene.dataset.journeyPinned = String(pinned)
      const entry = scene.querySelector<HTMLElement>(index === 0 ? 'h1' : 'h2')!
      const exitCandidates = Array.from(scene.querySelectorAll<HTMLElement>(['h1', '.protocol-row h3', '.provider-identity', '.step-body h3', 'h2', 'h2', 'h2'][index]!))
      const exit = exitCandidates.at(-1) ?? entry
      entry.dataset.journeyEntry = 'true'
      exit.dataset.journeyExit = 'true'
      const title = (element: HTMLElement): JourneyTitle => {
        const style = getComputedStyle(element)
        const fontSize = parseFloat(style.fontSize)
        const lines = new Map<number, { text: string; left: number; right: number }>()
        const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
        const range = document.createRange()
        let node: Node | null
        while ((node = walker.nextNode())) {
          for (let i = 0; i < (node.textContent?.length ?? 0); i++) {
            range.setStart(node, i)
            range.setEnd(node, i + 1)
            const rect = range.getBoundingClientRect?.()
            if (!rect?.width) continue
            const key = Math.round(rect.top)
            const line = lines.get(key) ?? { text: '', left: rect.left, right: rect.right }
            line.text += node.textContent![i]
            line.left = Math.min(line.left, rect.left)
            line.right = Math.max(line.right, rect.right)
            lines.set(key, line)
          }
        }
        const bounds = box(element)
        const paddingTop = parseFloat(style.paddingTop) || 0
        const paddingBottom = parseFloat(style.paddingBottom) || 0
        const paddingLeft = parseFloat(style.paddingLeft) || 0
        const paddingRight = parseFloat(style.paddingRight) || 0
        return {
          x: bounds.x + paddingLeft, y: bounds.y + paddingTop,
          width: bounds.width - paddingLeft - paddingRight, height: bounds.height - paddingTop - paddingBottom,
          text: lines.size ? Array.from(lines.values()).map(line => line.text.trim()).join('\n') : element.textContent?.trim() ?? '',
          fontSize, lineHeight: parseFloat(style.lineHeight) / fontSize, weight: Number(style.fontWeight) || 400,
          lineWidth: lines.size ? Math.max(...Array.from(lines.values()).map(line => line.right - line.left)) : box(element).width,
        }
      }
      const itemSelector = ['', '.protocol-path code', '.provider-row .model-featured .model-id', '.step-action', '.assurance-cell h3', '.control-feature-entry h3', '.ready-letter'][index]
      const items = itemSelector ? Array.from(scene.querySelectorAll<HTMLElement>(itemSelector)).map(element => {
        element.dataset.journeyItem = 'true'
        const style = getComputedStyle(element)
        return {
          ...title(element), color: style.color, fontFamily: style.fontFamily,
          caption: element.closest('.provider-row')?.querySelector('.provider-identity')?.textContent?.trim() ?? element.closest('.pipeline-step-item')?.querySelector('.step-index')?.textContent?.trim(),
          icon: element.closest<HTMLElement>('[data-journey-icon]')?.dataset.journeyIcon as JourneyItem['icon'],
        }
      }) : []
      return {
        items, pinned, corePinned: pinned, entryTitle: title(entry), exitTitle: title(exit),
        shape: {
          core: { ...coreBox, fontSize, label, fill: index === 0 || index === 6 ? 1 : 0, outline: index === 3 ? 1 : 0, radius: index === 3 ? coreBox.width / 2 : index === 6 ? 4 : 0, opacity: index === 4 || index === 5 ? 0 : 1 },
          lines, lineOpacity: index === 0 ? 0.8 : index === 6 ? 0 : 0.32,
        },
      }
    })
    segments.value = createJourneyTimeline(heights, visibleHeight)
    stage.classList.remove('journey-measuring')
    measuring = false
    update()
  }

  async function refreshMode() {
    const stage = stageRef.value
    const shouldEnable = !media?.matches && window.innerHeight >= 560 && !!stage?.getBoundingClientRect().width
    const changed = enabled.value !== shouldEnable
    const currentPhase = enabled.value ? phase.value : elements().reduce((last, scene, index) => scene.getBoundingClientRect().top <= 88 ? index : last, 0)
    const readOffset = enabled.value ? sample.value.offsets[currentPhase] ?? 0 : Math.max(0, 64 - (elements()[currentPhase]?.getBoundingClientRect().top ?? 64))
    enabled.value = shouldEnable
    await nextTick()
    if (disposed) return
    measure()
    if (changed && currentPhase > 0) {
      const header = window.innerWidth <= 760 ? 56 : 64
      const target = shouldEnable ? trackRef.value : elements()[currentPhase]
      if (target) {
        const offset = shouldEnable ? (segments.value[currentPhase]?.readStart ?? 0) + Math.min(readOffset, segments.value[currentPhase]?.overflow ?? 0) : readOffset
        window.scrollTo({ top: window.scrollY + target.getBoundingClientRect().top - header + offset, behavior: 'instant' })
        update()
      }
    }
  }
  function resize() { void refreshMode() }

  const frame = computed<JourneyFrame | null>(() => {
    if (!layouts.value.length || !enabled.value) return null
    const current = sample.value
    const dark = isDark()
    const palette = (index: number) => {
      const inverse = index === 1 || index === 3 || index === 6
      return {
        paper: inverse ? '#17181b' : index === 5 ? dark ? '#242529' : '#f0f0f2' : dark ? '#1c1d20' : '#fafafa',
        ink: inverse || dark ? '#f5f5f6' : '#1b1c1f',
        accent: inverse || dark ? '#8aa5ff' : '#315be8',
      }
    }
    const fromPalette = palette(current.index)
    const toPalette = palette(current.next)
    const shape = (index: number): JourneyShape => {
      const layout = layouts.value[index]!
      const offset = current.offsets[index] ?? 0
      return {
        ...layout.shape,
        core: { ...layout.shape.core, y: layout.shape.core.y - (layout.corePinned ? 0 : offset) },
        lines: layout.shape.lines.map(points => points.map(point => ({ x: point.x, y: point.y - offset }))),
      }
    }
    const from = shape(current.index)
    const to = shape(current.next)
    const width = stageRef.value?.clientWidth ?? 0
    const height = viewport.value
    const curtainOrigins: JourneyBox[] = [
      { ...from.core, y: Math.max(0, Math.min(height - from.core.height, from.core.y)) },
      { x: 0, y: height, width, height: 0 },
      { x: width, y: 0, width: 0, height },
      { x: 0, y: 0, width, height: 0 },
      { x: width / 2, y: height / 2, width: 0, height: 0 },
      { x: 0, y: 0, width: 0, height },
    ]
    const surface = {
      ...interpolateBox(curtainOrigins[current.index] ?? from.core, { x: 0, y: 0, width, height }, ease((current.blend - 0.08) / 0.70)),
      color: toPalette.paper,
    }
    const fromTitle = { ...layouts.value[current.index]!.exitTitle, y: layouts.value[current.index]!.exitTitle.y - (current.offsets[current.index] ?? 0) }
    const toTitle = { ...layouts.value[current.next]!.entryTitle, y: layouts.value[current.next]!.entryTitle.y - (current.offsets[current.next] ?? 0) }
    const onSurface = (box: JourneyBox) => box.x + box.width / 2 >= surface.x && box.x + box.width / 2 <= surface.x + surface.width && box.y + box.height / 2 >= surface.y && box.y + box.height / 2 <= surface.y + surface.height
    const coreOnSurface = current.blend > 0 && onSurface(morphCoreBox(from.core, to.core, current.blend, viewport.value))
    const items = (index: number) => layouts.value[index]!.items.map(item => ({ ...item, y: item.y - (current.offsets[index] ?? 0) }))
    return {
      chapter: current.index, width, fromItems: items(current.index), toItems: items(current.next),
      fromTitle, toTitle, surface, height,
      from, to, blend: current.blend,
      paper: fromPalette.paper,
      ink: coreOnSurface ? toPalette.ink : fromPalette.ink,
      accent: coreOnSurface ? toPalette.accent : fromPalette.accent,
      progress: distance.value / Math.max(1, total.value),
    }
  })
  const stageStyle = computed<CSSProperties>(() => enabled.value ? { backgroundColor: frame.value?.paper } : {})

  function sceneStyle(index: number): CSSProperties {
    if (!enabled.value) return {}
    const current = sample.value
    const entering = index === current.next && index !== current.index
    const active = index === current.index || (entering && current.blend > 0)
    const enter = entering ? 1 - ease((current.blend - 0.78) / 0.22) : 0
    const exit = index === current.index ? ease(current.blend / 0.36) : 0
    const offset = current.offsets[index] ?? 0
    return {
      visibility: active ? 'visible' : 'hidden',
      transform: `translate3d(0, ${-offset}px, 0)`,
      '--journey-enter': enter,
      '--journey-exit': exit,
      '--journey-copy': 1 - Math.max(enter, exit),
      '--journey-entry-title': entering && current.blend > 0 ? 0 : 1,
      '--journey-exit-title': index === current.index && current.blend > 0 ? 0 : 1,
      '--journey-item-copy': current.blend > 0 && (index > 1 || (index === 1 && !entering)) ? 0 : 1,
      '--journey-read': `${layouts.value[index]?.pinned ? offset : 0}px`,
    }
  }
  function inactive(index: number) {
    return enabled.value && (index !== phase.value || (sample.value.blend > 0.15 && sample.value.blend < 0.9))
  }

  async function scrollToSection(event: MouseEvent) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    const link = event.currentTarget as HTMLAnchorElement
    const target = document.getElementById(link.hash.slice(1))
    if (!target) return
    event.preventDefault()
    if (enabled.value && trackRef.value) {
      const scenes = elements()
      const index = scenes.findIndex(scene => scene === target || scene.contains(target))
      const segment = segments.value[index]
      if (!segment) return
      const scene = scenes[index]!
      const innerOffset = target === scene ? 0 : Math.max(0, target.getBoundingClientRect().top - scene.getBoundingClientRect().top - 32)
      const header = window.innerWidth <= 760 ? 56 : 64
      const top = window.scrollY + trackRef.value.getBoundingClientRect().top - header
      window.scrollTo({ top: top + segment.readStart + Math.min(innerOffset, segment.overflow), behavior: 'instant' })
      update()
      await nextTick()
    } else {
      target.scrollIntoView({ behavior: 'instant', block: 'start' })
    }
    target.tabIndex = -1
    target.focus({ preventScroll: true })
  }

  function handleFocus(event: FocusEvent) {
    if (!enabled.value || !(event.target instanceof HTMLElement) || !stageRef.value || !trackRef.value) return
    const stage = stageRef.value
    stage.scrollTop = 0
    stage.scrollLeft = 0
    const target = event.target
    const index = elements().findIndex(scene => scene.contains(target))
    const segment = segments.value[index]
    if (!segment || index !== phase.value) return
    const rect = target.getBoundingClientRect()
    const stageRect = stage.getBoundingClientRect()
    if (rect.top >= stageRect.top && rect.bottom <= stageRect.bottom - 40) return
    const scene = elements()[index]!
    const offset = Math.min(segment.overflow, Math.max(0, rect.top - scene.getBoundingClientRect().top - 32))
    window.scrollTo({ top: window.scrollY + trackRef.value.getBoundingClientRect().top - stageRect.top + segment.readStart + offset, behavior: 'instant' })
    update()
  }

  onMounted(async () => {
    media = window.matchMedia('(prefers-reduced-motion: reduce)')
    media.addEventListener?.('change', resize)
    await refreshMode()
    if (disposed) return
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(measure)
      if (stageRef.value) observer.observe(stageRef.value)
      elements().forEach(scene => observer?.observe(scene))
    }
    window.addEventListener('scroll', schedule, { passive: true })
    stageRef.value?.addEventListener('focusin', handleFocus)
    window.addEventListener('resize', resize, { passive: true })
    void document.fonts?.ready.then(() => { if (!disposed) measure() })
  })
  watch([isDark, contentKey], () => { void nextTick(measure) })
  onUnmounted(() => {
    disposed = true
    observer?.disconnect()
    media?.removeEventListener?.('change', resize)
    window.removeEventListener('scroll', schedule)
    stageRef.value?.removeEventListener('focusin', handleFocus)
    window.removeEventListener('resize', resize)
    if (raf !== undefined) window.cancelAnimationFrame(raf)
  })

  return { trackRef, stageRef, enabled, trackStyle, stageStyle, frame, phase, sceneStyle, inactive, scrollToSection, sceneIds }
}
