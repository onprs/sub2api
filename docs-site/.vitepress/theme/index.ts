import type { Theme } from 'vitepress'
import DefaultTheme from 'vitepress/theme'
import HomePage from '../components/HomePage.vue'
import QuotaWindowDiagram from '../components/QuotaWindowDiagram.vue'
import Layout from './Layout.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  Layout,
  enhanceApp({ app }) {
    app.component('HomePage', HomePage)
    app.component('QuotaWindowDiagram', QuotaWindowDiagram)
  }
} satisfies Theme
