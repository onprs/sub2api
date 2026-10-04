package zcode

// 来源：src/proxy/identity.ts、upstream.ts；固定 commit 见 UpstreamCommit。
import (
	"net/http"
	"runtime"
	"strings"
)

type Identity struct {
	AppVersion string
	DeviceMid  string
	Platform   string
	OSVersion  string
	Language   string
	Timezone   string
}

func printable(v string) string {
	v = strings.TrimSpace(v)
	for _, r := range v {
		if r < 32 || r > 126 {
			return ""
		}
	}
	return v
}
func DefaultIdentity(version, device string) Identity {
	if version == "" {
		version = DefaultAppVersion
	}
	platform := runtime.GOOS
	if platform == "windows" {
		platform = "win32"
	}
	if platform == "darwin" {
		platform = "darwin"
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	return Identity{AppVersion: version, DeviceMid: device, Platform: platform + "-" + arch, Language: "unknown", Timezone: "UTC"}
}
func (id Identity) Version() string { return printable(id.AppVersion) }
func (id Identity) Headers(llm bool) http.Header {
	h := http.Header{}
	v := id.Version()
	if v == "" {
		v = "unknown"
	} else {
		h.Set("X-ZCode-App-Version", v)
	}
	h.Set("User-Agent", "ZCode/"+v)
	h.Set("HTTP-Referer", DefaultOrigin)
	h.Set("X-Title", "Z Code@cli")
	h.Set("X-Release-Channel", "production")
	language := printable(id.Language)
	if language == "" {
		language = "unknown"
	}
	h.Set("X-Client-Language", language)
	timezone := printable(id.Timezone)
	if timezone == "" {
		timezone = "unknown"
	}
	h.Set("X-Client-Timezone", timezone)
	if p := printable(id.Platform); p != "" {
		h.Set("X-Platform", p)
	}
	category := "linux"
	if strings.HasPrefix(id.Platform, "win32-") {
		category = "windows"
	}
	if strings.HasPrefix(id.Platform, "darwin-") {
		category = "macos"
	}
	h.Set("X-Os-Category", category)
	if v := printable(id.OSVersion); v != "" {
		h.Set("X-Os-Version", v)
	}
	if llm {
		h.Set("X-ZCode-Agent", "glm")
	} else if v := printable(id.DeviceMid); v != "" {
		h.Set("X-Device-Mid", v)
	}
	return h
}
