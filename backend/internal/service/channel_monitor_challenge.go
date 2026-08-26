package service

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
)

// monitorChallengePromptTemplate 1:1 复刻 BingZi-233/check-cx 的 few-shot 模板。
const monitorChallengePromptTemplate = `Calculate and respond with ONLY the number, nothing else.

Q: 3 + 5 = ?
A: 8

Q: 12 - 7 = ?
A: 5

Q: %d %s %d = ?
A:`

const monitorExactChallengePromptTemplate = `Reply with ONLY this exact check code:
%s`

// monitorChallengeNumberRegex 提取响应中的所有整数（含负号）。
var monitorChallengeNumberRegex = regexp.MustCompile(`-?\d+`)

// monitorChallenge 一次 challenge 的 prompt + 期望答案。
type monitorChallenge struct {
	Prompt   string
	Expected string
	Exact    bool
}

func generateChallengeForProvider(provider string) monitorChallenge {
	if provider == MonitorProviderAntigravityGemini || provider == MonitorProviderClinePass || provider == MonitorProviderOpenCodeGo || provider == MonitorProviderCommandCode {
		return generateExactChallenge()
	}
	return generateChallenge()
}

// generateChallenge 生成一次随机算术 challenge：
//   - 随机两个 [monitorChallengeMin, monitorChallengeMax] 整数
//   - 50% 加 / 50% 减；减法用 max - min 保证非负
//   - 渲染 few-shot 模板
//
// 不强求加密随机：math/rand/v2 足够分散，避免 crypto/rand 的开销。
func generateChallenge() monitorChallenge {
	a := randIntInRange(monitorChallengeMin, monitorChallengeMax)
	b := randIntInRange(monitorChallengeMin, monitorChallengeMax)

	if rand.IntN(2) == 0 { //nolint:gosec // 仅用于生成测试问题，无安全影响
		// 加法
		return monitorChallenge{
			Prompt:   fmt.Sprintf(monitorChallengePromptTemplate, a, "+", b),
			Expected: strconv.Itoa(a + b),
		}
	}

	// 减法，保证非负
	hi, lo := a, b
	if lo > hi {
		hi, lo = lo, hi
	}
	return monitorChallenge{
		Prompt:   fmt.Sprintf(monitorChallengePromptTemplate, hi, "-", lo),
		Expected: strconv.Itoa(hi - lo),
	}
}

func generateExactChallenge() monitorChallenge {
	code := strconv.Itoa(randIntInRange(100000, 999999))
	return monitorChallenge{
		Prompt:   fmt.Sprintf(monitorExactChallengePromptTemplate, code),
		Expected: code,
		Exact:    true,
	}
}

// randIntInRange 返回 [min, max] 闭区间的随机整数。
func randIntInRange(minVal, maxVal int) int {
	if maxVal <= minVal {
		return minVal
	}
	return minVal + rand.IntN(maxVal-minVal+1) //nolint:gosec
}

// validateChallenge 在响应文本中查找 expected 整数答案，返回是否通过校验。
func validateChallenge(responseText, expected string) bool {
	if responseText == "" || expected == "" {
		return false
	}
	matches := monitorChallengeNumberRegex.FindAllString(responseText, -1)
	for _, m := range matches {
		if m == expected {
			return true
		}
	}
	return false
}

func validateMonitorChallenge(responseText string, challenge monitorChallenge) bool {
	if challenge.Exact {
		return normalizeExactChallengeResponse(responseText) == challenge.Expected
	}
	return validateChallenge(responseText, challenge.Expected)
}

func normalizeExactChallengeResponse(responseText string) string {
	normalized := strings.TrimSpace(responseText)
	normalized = strings.Trim(normalized, "`'\" \t\r\n")
	normalized = strings.TrimSuffix(normalized, ".")
	return strings.TrimSpace(normalized)
}
