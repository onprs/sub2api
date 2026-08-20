package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const apiKeyRoutingHealthMaxGroups = 200

type apiKeyRoutingHealthItem struct {
	GroupID          int64      `json:"group_id"`
	Status           string     `json:"status"`
	SuccessRate      *float64   `json:"success_rate"`
	AverageLatencyMs *int64     `json:"average_latency_ms"`
	SampleCount      int64      `json:"sample_count"`
	LastObservedAt   *time.Time `json:"last_observed_at"`
}

type apiKeyRoutingHealthResponse struct {
	WindowMinutes int                       `json:"window_minutes"`
	Items         []apiKeyRoutingHealthItem `json:"items"`
}

// GetAPIKeyRoutingHealth 返回候选分组的近 30 分钟真实路由健康快照。
func (h *GatewayHandler) GetAPIKeyRoutingHealth(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	groupIDs, err := parseAPIKeyRoutingHealthGroupIDs(c.Query("group_ids"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if h == nil || h.gatewayService == nil || h.apiKeyService == nil {
		response.InternalError(c, "Routing health service is unavailable")
		return
	}

	role, _ := middleware2.GetUserRoleFromContext(c)
	if role != service.RoleAdmin {
		groupIDs, err = h.apiKeyService.FilterAccessibleRoutingHealthGroupIDs(
			c.Request.Context(),
			subject.UserID,
			groupIDs,
		)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	snapshots := h.gatewayService.GetAPIKeyRoutingHealthSnapshots(c.Request.Context(), groupIDs)
	items := make([]apiKeyRoutingHealthItem, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, apiKeyRoutingHealthItem{
			GroupID:          snapshot.GroupID,
			Status:           snapshot.Status,
			SuccessRate:      snapshot.SuccessRate,
			AverageLatencyMs: snapshot.AverageLatencyMs,
			SampleCount:      snapshot.SampleCount,
			LastObservedAt:   snapshot.LastObservedAt,
		})
	}
	response.Success(c, apiKeyRoutingHealthResponse{
		WindowMinutes: service.APIKeyRoutingHealthWindowMinutes,
		Items:         items,
	})
}

func parseAPIKeyRoutingHealthGroupIDs(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []int64{}, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > apiKeyRoutingHealthMaxGroups {
		return nil, fmt.Errorf("at most %d group IDs are allowed", apiKeyRoutingHealthMaxGroups)
	}
	groupIDs := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		groupID, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || groupID <= 0 {
			return nil, fmt.Errorf("invalid group ID")
		}
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		groupIDs = append(groupIDs, groupID)
	}
	return groupIDs, nil
}
