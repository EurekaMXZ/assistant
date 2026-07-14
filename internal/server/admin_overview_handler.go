package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *API) handleGetAdminOverview(c *gin.Context) {
	result, err := a.useCases.Overview.GetAdminOverview(c.Request.Context(), currentUser(c))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
