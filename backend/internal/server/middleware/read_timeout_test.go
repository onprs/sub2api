package middleware

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestReadTimeoutClosesSlowUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const timeout = 75 * time.Millisecond
	router := gin.New()
	router.Use(ReadTimeout(timeout))
	router.POST("/slow", func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusRequestTimeout)
			return
		}
		c.Status(http.StatusNoContent)
	})

	server := httptest.NewServer(router)
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	conn, err := net.DialTimeout("tcp", serverURL.Host, time.Second)
	if err != nil {
		t.Fatalf("dial test server: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}

	request := fmt.Sprintf(
		"POST /slow HTTP/1.1\r\nHost: %s\r\nContent-Length: 10\r\nConnection: close\r\n\r\nx",
		serverURL.Host,
	)
	started := time.Now()
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatalf("write partial request: %v", err)
	}

	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read timeout response: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusRequestTimeout)
	}
	elapsed := time.Since(started)
	if elapsed < timeout/2 || elapsed > time.Second {
		t.Fatalf("slow upload reclaimed after %s, expected near %s", elapsed, timeout)
	}
}

func TestReadTimeoutDoesNotLimitResponseDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const timeout = 25 * time.Millisecond
	router := gin.New()
	router.Use(ReadTimeout(timeout))
	router.POST("/long-response", func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		time.Sleep(3 * timeout)
		c.String(http.StatusOK, "done")
	})

	server := httptest.NewServer(router)
	defer server.Close()

	response, err := server.Client().Post(
		server.URL+"/long-response",
		"text/plain",
		strings.NewReader("complete body"),
	)
	if err != nil {
		t.Fatalf("post complete request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "done" {
		t.Fatalf("status/body = %d/%q, want 200/done", response.StatusCode, body)
	}
}
