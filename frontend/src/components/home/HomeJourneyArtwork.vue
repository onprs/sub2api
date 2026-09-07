<template>
  <div class="journey-artwork" :data-motif="journeyMotifs[frame.chapter]" aria-hidden="true" :style="{ backgroundColor: frame.paper }">
    <div v-if="frame.blend > 0" class="journey-surface" :style="surfaceStyle" />
    <canvas ref="canvasRef" class="journey-lines" />
    <div v-if="frame.blend > 0" class="journey-title" :style="titleStyle">
      <span :style="{ opacity: frame.chapter === 0 ? 1 - ease((frame.blend - 0.40) / 0.10) : 1 - ease(frame.blend / 0.05) }">{{ frame.fromTitle.text }}</span>
      <span :style="{ opacity: ease((frame.blend - 0.50) / 0.10) }">{{ frame.toTitle.text }}</span>
    </div>
    <div v-show="frame.blend > 0" class="journey-pieces">
      <div v-for="(piece, index) in pieces" :key="index" class="journey-piece" :style="pieceStyle(piece)">
        <i v-if="frame.chapter === 1" class="journey-piece-material" :style="{ opacity: piece.backgroundOpacity, borderRadius: `${piece.radius}px`, backgroundColor: piecePalette(index).paper }" />
        <div class="journey-piece-label" :style="labelStyle(piece, index, false)">
          <Icon v-if="piece.from.icon" :name="piece.from.icon" :style="{ width: `${piece.iconSize}px`, height: `${piece.iconSize}px`, opacity: piece.material }" />
          <small v-if="piece.from.caption" :style="captionStyle(piece)">{{ piece.from.caption }}</small>
          <span :style="{ opacity: labelVisibility }">{{ piece.from.text }}</span>
        </div>
        <div class="journey-piece-label" :style="labelStyle(piece, index, true)">
          <Icon v-if="piece.to.icon" :name="piece.to.icon" :style="{ width: `${piece.iconSize}px`, height: `${piece.iconSize}px`, opacity: piece.material }" />
          <small v-if="piece.to.caption" :style="captionStyle(piece)">{{ piece.to.caption }}</small>
          <span :style="{ opacity: labelVisibility }">{{ piece.to.text }}</span>
        </div>
      </div>
    </div>
    <div class="journey-core" :style="coreStyle">
      <span :style="{ opacity: sameLabel ? 1 : 1 - ease((frame.blend - 0.30) / 0.20) }">{{ frame.from.core.label }}</span>
      <span v-if="!sameLabel" :style="{ opacity: ease((frame.blend - 0.50) / 0.20) }">{{ frame.to.core.label }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch, type CSSProperties } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { ease, mix, mixColor, morphCoreBox, type JourneyFrame } from './homeJourney'
import { journeyMotifs, journeyTitleBox, sampleJourneyPieces, type JourneyPiece } from './homeJourneyMorph'

const props = defineProps<{ frame: JourneyFrame }>()
const canvasRef = ref<HTMLCanvasElement | null>(null)
let observer: ResizeObserver | undefined
const pieces = computed(() => sampleJourneyPieces(props.frame))
const labelVisibility = computed(() => props.frame.chapter === 4 ? 1 - ease(props.frame.blend / 0.2) * (1 - ease((props.frame.blend - 0.62) / 0.16)) : 1)
function captionStyle(piece: JourneyPiece): CSSProperties {
  return { opacity: piece.material, fontSize: `${piece.captionSize}px`, height: `${piece.captionSize}px`, lineHeight: 1, fontFamily: 'Arial, sans-serif', fontWeight: 500 }
}
function piecePalette(index: number) {
  return index % 3 === 0 ? { paper: '#315be8', ink: '#ffffff' } : index % 3 === 1 ? { paper: '#e4e9fa', ink: '#1b1c1f' } : { paper: '#dedfe3', ink: '#1b1c1f' }
}
function pieceStyle(piece: JourneyPiece): CSSProperties {
  return {
    transform: `translate3d(${piece.box.x}px, ${piece.box.y}px, 0) perspective(900px) rotateX(${piece.fold}deg) rotate(${piece.rotation}deg)`,
    mixBlendMode: props.frame.chapter !== 1 && piece.material > 0.1 ? 'difference' : 'normal',
    width: `${piece.box.width}px`, height: `${piece.box.height}px`, opacity: piece.opacity,
  }
}
function labelStyle(piece: JourneyPiece, index: number, incoming: boolean): CSSProperties {
  const item = incoming ? piece.to : piece.from
  return {
    padding: `${piece.padding}px`, gap: `${6 * piece.material}px`,
    justifyContent: piece.material > 0.1 ? 'center' : 'flex-start',
    opacity: incoming ? piece.toOpacity : piece.fromOpacity,
    fontFamily: item.fontFamily, fontWeight: item.weight, fontSize: `${piece.fontSize}px`, lineHeight: item.lineHeight,
    color: props.frame.chapter === 1 ? piece.material > 0.5 ? piecePalette(index).ink : item.color : piece.material > 0.1 ? '#ffffff' : item.color,
  }
}
const sameLabel = computed(() => props.frame.from.core.label === props.frame.to.core.label)
const surfaceStyle = computed<CSSProperties>(() => {
  const { surface } = props.frame
  return { transform: `translate3d(${surface.x}px, ${surface.y}px, 0)`, width: `${surface.width}px`, height: `${surface.height}px`, background: surface.color }
})
const titleGeometry = computed(() => {
  const { fromTitle, toTitle, blend } = props.frame
  const amount = ease(blend)
  const box = journeyTitleBox(props.frame)
  const active = blend < 0.5 ? fromTitle : toTitle
  const intendedSize = mix(fromTitle.fontSize, toTitle.fontSize, amount)
  const lineHeight = mix(fromTitle.lineHeight, toTitle.lineHeight, amount)
  const fontSize = Math.min(intendedSize, box.width / Math.max(1, active.lineWidth) * active.fontSize, box.height / active.text.split('\n').length / lineHeight)
  return { ...box, height: active.text.split('\n').length * fontSize * lineHeight, fontSize, lineHeight }
})
const titleStyle = computed<CSSProperties>(() => {
  const { fromTitle, toTitle, blend } = props.frame
  const amount = ease(blend)
  const box = titleGeometry.value
  return {
    transform: `translate3d(${box.x}px, ${box.y}px, 0)`, width: `${box.width}px`,
    fontSize: `${box.fontSize}px`,
    lineHeight: box.lineHeight,
    fontWeight: mix(fromTitle.weight, toTitle.weight, amount),
  }
})
const coreStyle = computed<CSSProperties>(() => {
  const { from, to, blend, ink, accent } = props.frame
  const amount = ease(blend)
  const rect = morphCoreBox(from.core, to.core, blend, props.frame.height)
  const title = titleGeometry.value
  const gap = Math.max(title.x - rect.x - rect.width, rect.x - title.x - title.width, title.y - rect.y - rect.height, rect.y - title.y - title.height)
  const clearance = blend > 0 && blend < 1 ? ease((gap - 16) / 16) : 1
  const fill = mix(from.core.fill, to.core.fill, amount)
  const background = mixColor(
    from.core.fill === 1 && from.core.label === '' ? '#315be8' : accent,
    to.core.fill === 1 && to.core.label === '' ? '#315be8' : accent,
    amount,
  )
  return {
    transform: `translate3d(${rect.x}px, ${rect.y}px, 0) rotate(${Math.sin(amount * Math.PI) * -6}deg)`,
    width: `${rect.width}px`, height: `${rect.height}px`,
    borderRadius: `${mix(from.core.radius, to.core.radius, amount)}px`,
    backgroundColor: `${background}${Math.round(fill * 255).toString(16).padStart(2, '0')}`,
    borderColor: `${accent}${Math.round(mix(from.core.outline, to.core.outline, amount) * 255).toString(16).padStart(2, '0')}`,
    color: fill > 0.5 ? '#ffffff' : ink,
    opacity: mix(from.core.opacity, to.core.opacity, amount) * clearance * (props.frame.chapter === 0 ? 1 : 1 - ease(blend / 0.2) * (1 - ease((blend - 0.85) / 0.15))),
    fontSize: `${mix(from.core.fontSize, to.core.fontSize, amount)}px`,
  }
})

function draw() {
  const canvas = canvasRef.value
  if (!canvas) return
  const { width, height } = canvas.getBoundingClientRect()
  if (!width || !height) return
  const context = canvas.getContext('2d')
  if (!context) return
  const ratio = Math.min(window.devicePixelRatio || 1, 2)
  if (canvas.width !== Math.round(width * ratio) || canvas.height !== Math.round(height * ratio)) {
    canvas.width = Math.round(width * ratio)
    canvas.height = Math.round(height * ratio)
  }
  context.setTransform(ratio, 0, 0, ratio, 0, 0)
  context.clearRect(0, 0, width, height)
  if (props.frame.chapter !== 0) return
  const { from, to, blend, accent, progress } = props.frame
  const amount = ease(blend)
  const opening = Math.sin(amount * Math.PI)
  const opacity = Math.max(mix(from.lineOpacity, to.lineOpacity, amount), opening * 0.85)
  context.lineWidth = 1 + opening * 0.6
  context.strokeStyle = accent
  context.lineJoin = 'round'
  context.globalAlpha = opacity

  // 同一组折线在相邻章节的真实锚点之间展开，端点保持严格连续。
  from.lines.forEach((line, index) => {
    const target = to.lines[index] ?? line
    const direction = index % 2 ? 1 : -1
    context.beginPath()
    line.forEach((point, vertex) => {
      const destination = target[vertex] ?? point
      const x = mix(point.x, destination.x, amount)
      const y = mix(point.y, destination.y, amount) + Math.sin(vertex / 3 * Math.PI) * opening * direction * Math.min(height * 0.055, 44)
      if (vertex === 0) context.moveTo(x, y)
      else context.lineTo(x, y)
    })
    context.stroke()
    if (opening > 0 || from.core.fill === 1) {
      context.setLineDash([18, 110])
      context.lineDashOffset = -progress * 1400 - index * 18
      context.globalAlpha = Math.min(1, opacity + 0.25)
      context.stroke()
      context.setLineDash([])
      context.globalAlpha = opacity
    }
  })
  if (blend > 0) {
    const title = titleGeometry.value
    context.clearRect(title.x - 12, title.y - 8, title.width + 24, title.height + 16)
  }
}
watch(() => props.frame, draw)
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined' && canvasRef.value) {
    observer = new ResizeObserver(draw)
    observer.observe(canvasRef.value)
  }
  draw()
})
onUnmounted(() => observer?.disconnect())
</script>

<style scoped>
.journey-artwork, .journey-lines { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; }
.journey-artwork { z-index: 1; overflow: hidden; }
.journey-surface, .journey-title { position: absolute; top: 0; left: 0; }
.journey-surface { will-change: transform, width, height; }
.journey-title { z-index: 2; display: grid; letter-spacing: 0; font-family: inherit; color: #ffffff; mix-blend-mode: difference; }
.journey-title > span { grid-area: 1 / 1; white-space: pre; overflow-wrap: normal; }
.journey-pieces { position: absolute; inset: 0; }
.journey-piece { position: absolute; top: 0; left: 0; transform-origin: center; will-change: transform; }
.journey-piece-material { position: absolute; inset: 0; }
.journey-piece-label { position: absolute; inset: 0; display: flex; flex-direction: column; justify-content: center; overflow: hidden; }
.journey-piece-label > span { white-space: pre; }
.journey-piece-label > small { white-space: nowrap; }
.journey-piece-label > svg { flex: 0 0 auto; }
.journey-core { position: absolute; top: 0; left: 0; display: grid; place-items: center; border: 1px solid transparent; line-height: 1; font-family: Arial, "Helvetica Neue", sans-serif; font-weight: 400; transform-origin: center; will-change: transform, width, height; }
.journey-core > span { grid-area: 1 / 1; white-space: nowrap; }
</style>
