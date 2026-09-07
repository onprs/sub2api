<template>
  <canvas ref="canvasRef" class="routing-artwork" aria-hidden="true" />
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{ progress: number; dark?: boolean }>()
const canvasRef = ref<HTMLCanvasElement | null>(null)
let observer: ResizeObserver | undefined
let motionQuery: MediaQueryList | undefined

function draw() {
  const canvas = canvasRef.value
  if (!canvas) return
  const { width, height } = canvas.getBoundingClientRect()
  if (!width || !height) return
  const context = canvas.getContext('2d')
  if (!context) return
  const ratio = Math.min(window.devicePixelRatio || 1, 2)
  canvas.width = Math.round(width * ratio)
  canvas.height = Math.round(height * ratio)
  context.scale(ratio, ratio)

  const mobile = window.innerWidth <= 760
  const compact = mobile && height < 680
  const left = mobile ? -width * 0.06 : width * 0.51
  const top = mobile ? height - (compact ? 246 : 286) : height * 0.44
  const unit = mobile ? Math.min(width * (compact ? 0.74 : 0.94), 420) : Math.min(width * 0.51, height * 0.66)
  const centerX = left + unit * 0.52
  const centerY = top + unit * 0.26
  const lineGap = unit * 0.013
  const reducedMotion = motionQuery?.matches ?? window.matchMedia('(prefers-reduced-motion: reduce)').matches
  const accent = props.dark ? '#8aa5ff' : '#315be8'
  const secondary = props.dark ? '#646b80' : '#acb7d7'
  const signal = props.dark ? '#f5f5f6' : '#315be8'
  const shift = reducedMotion ? 0 : Math.min(props.progress / 0.28, 1) * unit * 0.12

  // 四束输入穿过统一路由节点，再分发至六个模型来源。
  context.lineCap = 'butt'
  context.lineJoin = 'bevel'
  for (let group = 0; group < 4; group++) {
    for (let lane = 0; lane < 5; lane++) {
      const y = top + group * unit * 0.14 + lane * lineGap
      const endY = centerY + (group * 5 + lane - 9.5) * lineGap * 0.45
      context.beginPath()
      context.moveTo(left - unit * 0.06, y)
      context.lineTo(left + unit * (0.16 + group * 0.04), y)
      context.lineTo(centerX - unit * 0.065, endY)
      context.lineTo(centerX + unit * 0.08, endY)
      context.strokeStyle = group === 1 ? accent : secondary
      context.lineWidth = Math.max(1, unit * 0.0025)
      context.stroke()
      context.setLineDash([unit * 0.035, unit * 0.62])
      context.lineDashOffset = -shift - group * unit * 0.12
      context.strokeStyle = signal
      context.stroke()
      context.setLineDash([])
    }
  }

  for (let group = 0; group < 6; group++) {
    for (let lane = 0; lane < 3; lane++) {
      const startY = centerY + (group * 3 + lane - 8.5) * lineGap * 0.45
      const endY = top + group * unit * 0.092 + lane * lineGap
      context.beginPath()
      context.moveTo(centerX + unit * 0.085, startY)
      context.lineTo(centerX + unit * 0.20, startY)
      context.lineTo(centerX + unit * 0.39, endY)
      context.lineTo(width + unit * 0.06, endY)
      context.strokeStyle = group % 2 ? secondary : accent
      context.lineWidth = Math.max(1, unit * 0.0025)
      context.stroke()
    }
  }

  context.fillStyle = accent
  context.fillRect(centerX - unit * 0.095, centerY - unit * 0.105, unit * 0.21, unit * 0.21)
  context.fillStyle = props.dark ? '#17181b' : '#ffffff'
  context.font = `900 ${Math.round(unit * 0.066)}px monospace`
  context.textAlign = 'center'
  context.textBaseline = 'middle'
  context.fillText('/v1', centerX + unit * 0.01, centerY)
}

watch(() => [props.progress, props.dark], draw)
onMounted(() => {
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  motionQuery.addEventListener?.('change', draw)
  if (!canvasRef.value || typeof ResizeObserver === 'undefined') return
  observer = new ResizeObserver(draw)
  observer.observe(canvasRef.value)
  draw()
})
onUnmounted(() => {
  observer?.disconnect()
  motionQuery?.removeEventListener?.('change', draw)
})
</script>

<style scoped>
.routing-artwork {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}
</style>
