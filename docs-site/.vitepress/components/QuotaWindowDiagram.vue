<script setup lang="ts">
const windows = [
  { name: '5 小时', short: '5h', width: '24%', tone: 'mint', note: '短周期，用于控制突发消耗' },
  { name: '7 天', short: '7d', width: '58%', tone: 'amber', note: '中周期，限制一周内累计用量' },
  { name: '30 天', short: '30d', width: '100%', tone: 'coral', note: '长周期，限制一个完整周期的累计用量' }
]
</script>

<template>
  <figure class="quota-diagram" aria-labelledby="quota-diagram-title">
    <figcaption id="quota-diagram-title">
      <strong>三个窗口同时检查</strong>
      <span>任一已配置窗口耗尽，请求都会被限制到该窗口到期。</span>
    </figcaption>
    <div class="quota-tracks">
      <div v-for="window in windows" :key="window.short" class="quota-row">
        <div class="quota-label"><strong>{{ window.short }}</strong><span>{{ window.name }}</span></div>
        <div class="quota-track">
          <span :data-tone="window.tone" :style="{ width: window.width }"></span>
        </div>
        <small>{{ window.note }}</small>
      </div>
    </div>
    <p>窗口从权益首次实际使用开始记录；到期后，下一次请求会进入新的同长度窗口。具体重置时间以控制台显示为准。</p>
  </figure>
</template>
