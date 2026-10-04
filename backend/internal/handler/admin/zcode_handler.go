package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *AccountHandler) SetZCodeService(svc *service.ZCodeService) { h.zcodeService = svc }
func (h *AccountHandler) zcodeReady(c *gin.Context) bool {
	if h.zcodeService == nil {
		response.Error(c, http.StatusServiceUnavailable, "ZCode 服务不可用")
		return false
	}
	return true
}
func (h *AccountHandler) ZCodeLogin(c *gin.Context) {
	if !h.zcodeReady(c) {
		return
	}
	var input service.ZCodeLoginInput
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "授权参数无效")
		return
	}
	result, err := h.zcodeService.StartLogin(c.Request.Context(), adminActorScope(c), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *AccountHandler) ZCodePoll(c *gin.Context) {
	if !h.zcodeReady(c) {
		return
	}
	var input struct {
		ID string `json:"session_id"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "授权会话无效")
		return
	}
	result, err := h.zcodeService.PollLogin(c.Request.Context(), adminActorScope(c), input.ID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func zcodeAccountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "账号 ID 无效")
		return 0, false
	}
	return id, true
}
func (h *AccountHandler) ZCodeStatus(c *gin.Context) {
	if !h.zcodeReady(c) {
		return
	}
	id, ok := zcodeAccountID(c)
	if !ok {
		return
	}
	result, err := h.zcodeService.Status(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *AccountHandler) ZCodeQuota(c *gin.Context) {
	if !h.zcodeReady(c) {
		return
	}
	id, ok := zcodeAccountID(c)
	if !ok {
		return
	}
	result, err := h.zcodeService.QueryQuota(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *AccountHandler) ZCodePreview(c *gin.Context) { h.zcodeClaim(c, false) }
func (h *AccountHandler) ZCodeClaim(c *gin.Context)   { h.zcodeClaim(c, true) }
func (h *AccountHandler) zcodeClaim(c *gin.Context, execute bool) {
	if !h.zcodeReady(c) {
		return
	}
	id, ok := zcodeAccountID(c)
	if !ok {
		return
	}
	result, err := h.zcodeService.CheckClaim(c.Request.Context(), id, execute)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
