package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *MyResourceHandler) GetAccountGrokModelDiagnostics(c *gin.Context) {
	userID, ok := h.currentUser(c)
	if !ok {
		return
	}
	accountID, ok := parseInt64Param(c, "id")
	if !ok {
		return
	}
	diagnostics, err := h.userResourceService.GetAccountGrokModelDiagnostics(c.Request.Context(), userID, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, diagnostics)
}
