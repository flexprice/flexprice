package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/gin-gonic/gin"
)

type MeHandler struct{}

func NewMeHandler() *MeHandler {
	return &MeHandler{}
}

// Me always answers ok: the secret check runs before it, so a rejected secret never reaches here.
func (h *MeHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, admin.MeResponse{OK: true})
}
