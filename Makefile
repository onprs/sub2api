.PHONY: build build-backend build-frontend test test-backend test-frontend test-frontend-critical secret-scan verify-release-binary check-python

PNPM ?= corepack pnpm@9.15.9
PYTHON ?= $(shell if command -v python >/dev/null 2>&1; then command -v python; elif command -v python3 >/dev/null 2>&1; then command -v python3; fi)

FRONTEND_CRITICAL_VITEST := \
	src/api/__tests__/client.spec.ts \
	src/api/__tests__/tokenRefresh.spec.ts \
	src/api/__tests__/channelMonitorV2.spec.ts \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/views/user/__tests__/ChannelStatusView.mode.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts \
	src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts \
	src/features/channel-monitor-v2/__tests__/monitorFormat.spec.ts \
	src/features/channel-monitor-v2/__tests__/monitorZoom.spec.ts

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@$(PNPM) --dir frontend run build

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend:
	@$(PNPM) --dir frontend run lint:check
	@$(PNPM) --dir frontend run typecheck
	@$(MAKE) test-frontend-critical

test-frontend-critical:
	@$(PNPM) --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)

check-python:
	@test -n "$(PYTHON)" || (echo "Python 3.10+ is required; set PYTHON=/path/to/python" >&2; exit 2)
	@"$(PYTHON)" -c "import sys; sys.exit(0 if sys.version_info >= (3, 10) else 2)" || (echo "Python 3.10+ is required" >&2; exit 2)

secret-scan: check-python
	@"$(PYTHON)" tools/secret_scan.py

verify-release-binary: check-python
	@test -n "$(BINARY)" || (echo "Usage: make verify-release-binary BINARY=/path/to/sub2api [EXPECTED_SHA256=...]" >&2; exit 2)
	@"$(PYTHON)" tools/verify_release_binary.py "$(BINARY)" --profile onprs-subquota $(if $(EXPECTED_SHA256),--expected-sha256 "$(EXPECTED_SHA256)")
