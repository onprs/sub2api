# OnprsCodexApi 用户文档

## 本地开发

```bash
corepack pnpm install --frozen-lockfile
pnpm typecheck
pnpm build
pnpm preview
```

预览默认监听 `http://127.0.0.1:4174/`。生产只发布 `.vitepress/dist` 静态文件，不运行 Node 进程。

## 内容维护

- 用户文档源文件位于本目录各 Markdown 文件。
- 导航与站点配置位于 `.vitepress/config.ts`。
- 主题样式位于 `.vitepress/theme/custom.css`。
- 价格、库存和当前模型只链接控制台，不复制静态表。
- 示例必须使用占位 Key，截图和日志必须脱敏。

提交前运行：

```bash
pnpm typecheck
pnpm build
pnpm check:external
pnpm test:smoke
```
