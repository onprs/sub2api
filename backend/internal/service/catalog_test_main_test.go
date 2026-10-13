//go:build unit

package service

import (
	"errors"
	"net/http"
	"os"
	"testing"
)

// 默认目录使用内置快照；在线目录更新由注入独立 HTTP 客户端的目录测试覆盖。
// 避免完整单测因外部 GOAT 额度变化而改写其他计费测试的共享数据。
func TestMain(m *testing.M) {
	defaultCommandCodeCatalog = NewCommandCodeCatalog(&http.Client{Transport: offlineCommandCodeCatalogTransport{}})
	os.Exit(m.Run())
}

type offlineCommandCodeCatalogTransport struct{}

func (offlineCommandCodeCatalogTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("单元测试的默认 Command Code 目录禁用在线刷新")
}
